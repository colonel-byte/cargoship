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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/colonel-byte/cargoship/src/api"
	"github.com/colonel-byte/cargoship/src/pkg/packager/layout"
	"github.com/defenseunicorns/pkg/oci"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/pkg/images"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/utils"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
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

	// A package holds one set of blobs whatever the architecture count, so it pushes as a single
	// manifest. The index is what makes it resolvable per architecture, and it lists that one
	// manifest once for every architecture the package covers.
	arches := disLayout.Distro.Arches()
	if len(arches) == 0 {
		return ocispec.Descriptor{}, fmt.Errorf("package records no architecture, cannot publish")
	}

	copyOpts := r.OrasRemote.GetDefaultCopyOpts()
	copyOpts.Concurrency = opts.OCIConcurrency

	totalSize := disLayout.TotalSize()

	var publishedDesc ocispec.Descriptor
	err = retry.Do(
		func() error {
			l.Info("pushing package to registry", "destination", r.Repo().Reference.String(),
				"architectures", api.FormatArches(arches), "size", utils.ByteFormat(float64(totalSize), 2))

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

			return r.updateIndexForArches(ctx, r.Repo().Reference.Reference, publishedDesc, arches)
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

// updateIndexForArches tags an index at tag which points at publishedDesc once per architecture
// in arches. It replaces the OrasRemote.UpdateIndex it shadows, which only ever records the one
// platform the remote was built with and so cannot describe a package built for several.
//
// Entries for architectures the package does not cover are left alone, so publishing an amd64
// package and then an arm64 one of the same version leaves both resolvable under the same tag.
func (r *Remote) updateIndexForArches(ctx context.Context, tag string, publishedDesc ocispec.Descriptor, arches api.Arches) error {
	index, err := r.fetchIndex(ctx, tag)
	if err != nil {
		return err
	}

	for _, arch := range arches {
		platform := oci.PlatformForArch(string(arch))
		desc := ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageManifest,
			Digest:    publishedDesc.Digest,
			Size:      publishedDesc.Size,
			Platform:  &platform,
		}

		replaced := false
		for i, manifest := range index.Manifests {
			if manifest.Platform != nil && manifest.Platform.Architecture == string(arch) {
				index.Manifests[i] = desc
				replaced = true
				break
			}
		}
		if !replaced {
			index.Manifests = append(index.Manifests, desc)
		}
	}

	return r.pushIndex(ctx, index, tag)
}

// fetchIndex reads the index already tagged at tag. A tag that resolves to nothing, or to a plain
// manifest from a cargoship old enough to have tagged one directly, gives an empty index to add to.
func (r *Remote) fetchIndex(ctx context.Context, tag string) (*ocispec.Index, error) {
	empty := &ocispec.Index{
		MediaType: ocispec.MediaTypeImageIndex,
		Versioned: specs.Versioned{SchemaVersion: 2},
	}

	desc, err := r.Repo().Resolve(ctx, tag)
	if err != nil {
		if errors.Is(err, errdef.ErrNotFound) {
			return empty, nil
		}
		return nil, err
	}
	if desc.MediaType != ocispec.MediaTypeImageIndex {
		return empty, nil
	}

	desc, rc, err := r.Repo().FetchReference(ctx, tag)
	if err != nil {
		return nil, err
	}
	defer rc.Close() //nolint:errcheck // read-only body, nothing to do with a close error

	b, err := content.ReadAll(rc, desc)
	if err != nil {
		return nil, err
	}

	var index ocispec.Index
	if err := json.Unmarshal(b, &index); err != nil {
		return nil, err
	}
	return &index, nil
}

// pushIndex tags index at tag.
func (r *Remote) pushIndex(ctx context.Context, index *ocispec.Index, tag string) error {
	b, err := json.Marshal(index)
	if err != nil {
		return err
	}
	desc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageIndex, b)
	return r.Repo().Manifests().PushReference(ctx, desc, bytes.NewReader(b), tag)
}
