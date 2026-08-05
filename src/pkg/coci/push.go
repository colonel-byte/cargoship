// Copyright 2021 zarf authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from zarf:
// https://github.com/zarf-dev/zarf
//
// Modifications Copyright 2026 colonel-byte.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package coci

import (
	"context"
	"fmt"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/colonel-byte/cargoship/src/pkg/packager/layout"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/pkg/images"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/utils"
	"oras.land/oras-go/v2"
)

// OCITimestampFormat is the format used for the OCI timestamp annotation
const OCITimestampFormat = time.RFC3339

// PushPackage publishes the zarf package to the remote repository.
func (r *Remote) PushPackage(ctx context.Context, disLayout *layout.DistroLayout, opts PublishOptions) (_ ocispec.Descriptor, err error) {
	l := logger.From(ctx)

	start := time.Now()
	if opts.OCIConcurrency == 0 {
		opts.OCIConcurrency = DefaultConcurrency
	}

	// disallow infinite or negative
	if opts.Retries <= 0 {
		if opts.Retries < 0 {
			return ocispec.Descriptor{}, fmt.Errorf("retries cannot be negative")
		}
		l.Debug("retries set to default", "retries", DefaultRetries)
		opts.Retries = DefaultRetries
	}

	if !disLayout.IsPushable() {
		return ocispec.Descriptor{}, fmt.Errorf("package layout is not pushable; manifest cache must be computed before publishing")
	}

	copyOpts := r.OrasRemote.GetDefaultCopyOpts()
	copyOpts.Concurrency = opts.OCIConcurrency

	totalSize := disLayout.TotalSize()

	var publishedDesc ocispec.Descriptor
	err = retry.Do(
		func() error {
			l.Info("pushing package to registry", "destination", r.Repo().Reference.String(),
				"architecture", disLayout.Distro.Build.Architecture, "size", utils.ByteFormat(float64(totalSize), 2))

			trackedRemote := images.NewTrackedTarget(
				r.Repo(),
				totalSize,
				images.DefaultReport(r.Log(), "package publish in progress", r.Repo().Reference.String()),
			)
			trackedRemote.StartReporting(ctx)
			defer trackedRemote.StopReporting()

			var copyErr error
			publishedDesc, copyErr = oras.Copy(ctx, disLayout, disLayout.Digest(), trackedRemote, "", copyOpts)
			if copyErr != nil {
				return copyErr
			}

			return r.OrasRemote.UpdateIndex(ctx, r.Repo().Reference.Reference, publishedDesc)
		},
		retry.Attempts(uint(opts.Retries)),
		retry.Delay(defaultDelayTime),
		retry.MaxDelay(defaultMaxDelayTime),
		retry.DelayType(retry.BackOffDelay), // exponential backoff
		retry.LastErrorOnly(true),
		retry.Context(ctx),
		retry.OnRetry(func(n uint, err error) {
			// Only log retry if retries are enabled and this is not the last attempt
			if opts.Retries > 1 && n+1 < uint(opts.Retries) {
				l.Warn("retrying package push",
					"attempt", n+1,
					"maxAttempts", opts.Retries,
					"error", err,
				)
			}
		}),
	)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("publish failed: %w", err)
	}

	l.Info("completed package publish", "destination", r.Repo().Reference.String(),
		"duration", time.Since(start).Round(100*time.Millisecond))

	return publishedDesc, nil
}
