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

	"github.com/zarf-dev/zarf/src/pkg/logger"
	"oras.land/oras-go/v2"

	retry "github.com/avast/retry-go/v4"
)

// CopyPackage copies a zarf package from one OCI registry to another using ORAS with retry.
func CopyPackage(ctx context.Context, src *Remote, dst *Remote, opts PublishOptions) (err error) {
	l := logger.From(ctx)
	if opts.OCIConcurrency <= 0 {
		opts.OCIConcurrency = DefaultConcurrency
	}
	// disallow infinite or negative
	if opts.Retries <= 0 {
		if opts.Retries < 0 {
			return fmt.Errorf("retries cannot be negative")
		}
		l.Debug("retries set to default", "retries", DefaultRetries)
		opts.Retries = DefaultRetries
	}

	// Resolve the root digest of the source package (manifest or index)
	srcRoot, err := src.ResolveRoot(ctx)
	if err != nil {
		return err
	}
	srcRef := srcRoot.Digest.String()

	copyOpts := dst.OrasRemote.GetDefaultCopyOpts()
	copyOpts.Concurrency = opts.OCIConcurrency

	tag := src.Repo().Reference.Reference // keep the source tag on the destination
	if opts.Tag != "" {
		tag = opts.Tag
	}

	err = retry.Do(
		func() error {
			l.Info("copying package",
				"src", src.Repo().Reference.String(),
				"dst", dst.Repo().Reference.String(),
				"ref", srcRef,
			)

			source := src.Repo()      // implements oras.ReadOnlyTarget
			destination := dst.Repo() // implements oras.Target

			// 1) Copy by digest from source → destination
			publishedDesc, copyErr := oras.Copy(ctx, source, srcRef, destination, "", copyOpts)
			if copyErr != nil {
				return copyErr
			}

			// 2) Update/tag the destination index to the source tag
			return dst.OrasRemote.UpdateIndex(ctx, tag, publishedDesc)
		},
		retry.Attempts(uint(opts.Retries)),
		retry.Delay(defaultDelayTime),
		retry.MaxDelay(defaultMaxDelayTime),
		retry.DelayType(retry.BackOffDelay),
		retry.LastErrorOnly(true),
		retry.Context(ctx),
		retry.OnRetry(func(n uint, err error) {
			// Only log retry if retries are enabled and we're not on the last attempt
			if opts.Retries > 1 && n+1 < uint(opts.Retries) {
				l.Warn("retrying package copy",
					"attempt", n+1,
					"maxAttempts", opts.Retries,
					"error", err,
				)
			}
		}),
	)
	if err != nil {
		return fmt.Errorf("copy failed after retries: %w", err)
	}

	l.Info("package copied successfully",
		"source", src.Repo().Reference.String(),
		"destination", dst.Repo().Reference.String(),
		"tag", tag,
	)
	return nil
}
