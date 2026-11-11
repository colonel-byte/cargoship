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

package images

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/colonel-byte/cargoship/pkg/helpers"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/config"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/transform"
	"github.com/zarf-dev/zarf/src/pkg/utils"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/registry"
	orasRemote "oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
)

// PushOptions is the configuration for pushing images out of a package's OCI layout.
type PushOptions struct {
	OCIConcurrency        int
	PlainHTTP             bool
	InsecureSkipTLSVerify bool
	ResponseHeaderTimeout time.Duration
	// Retries is how many times a single image copy is attempted before the push fails.
	// Zero takes the same default the rest of the OCI paths use.
	Retries int
	// Username and Password authenticate the push against the target. Empty, which is the
	// usual case, falls back to the Docker configuration file.
	Username string
	Password string
	// DialContext opens the connection the push travels over. See ClientOptions.DialContext.
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)
}

// StaticCredential turns a username and password into something ClientOptions takes, and
// returns nil when neither was given -- which NewRegistryClient reads as "use the Docker
// configuration". It is exported because a mirror pushes files alongside images and both
// halves have to authenticate the same way.
func StaticCredential(host, username, password string) auth.CredentialFunc {
	if username == "" && password == "" {
		return nil
	}
	return auth.StaticCredential(host, auth.Credential{
		Username: username,
		Password: password,
	})
}

// Push copies every image in imageList out of the OCI layout at sourceDirectory and into target,
// which is a registry host optionally followed by a repository prefix -- "harbor.internal" or
// "harbor.internal/cargoship".
//
// The destination reference swaps the host and keeps everything else, so registry.k8s.io/pause:3.10
// is pushed as <target>/pause:3.10. That rule is not a preference: cargoship configures the engine
// to reach a mirror through a containerd hosts.toml, and containerd asks a mirror for the
// repository path unchanged. A reference rewritten any other way -- zarf's crc32 tag among them --
// would be one the node never asks for.
//
// An image the package holds for several architectures is tagged in the layout as an OCI image
// index, and the index is copied whole, so the registry ends up holding every architecture the
// package carried. That is the opposite of what the upload phase does with the same layout, where
// a docker-save tarball describes one image and a platform has to be chosen.
func Push(ctx context.Context, imageList []transform.Image, sourceDirectory, target string, opts PushOptions) error {
	if len(imageList) == 0 {
		return fmt.Errorf("image list is required")
	}
	if sourceDirectory == "" {
		return fmt.Errorf("source directory is required")
	}
	if target == "" {
		return fmt.Errorf("target registry is required")
	}
	if opts.Retries <= 0 {
		opts.Retries = config.ZarfDefaultRetries
	}
	if opts.OCIConcurrency <= 0 {
		opts.OCIConcurrency = 1
	}
	if opts.ResponseHeaderTimeout < 0 {
		opts.ResponseHeaderTimeout = 0
	}

	imageList = helpers.Unique(imageList)
	l := logger.From(ctx)
	pushStart := time.Now()

	// Read-only, and from the filesystem rather than oci.NewWithContext: the source is the
	// operator's package, and opening it for writing would let a mirror modify the artifact it
	// is only meant to be copying out of.
	src, err := oci.NewFromFS(ctx, os.DirFS(sourceDirectory))
	if err != nil {
		return fmt.Errorf("failed to read the image layout at %s: %w", sourceDirectory, err)
	}

	// target may carry a repository prefix -- "harbor.internal/cargoship" -- and only the host
	// is authenticated against, so it is split off before the reference is validated.
	host, _, _ := strings.Cut(target, "/")
	if err := (registry.Reference{Registry: host}).ValidateRegistry(); err != nil {
		return fmt.Errorf("invalid target registry %q: %w", target, err)
	}

	// Every destination is the one target host, so one entry pre-authenticates all of them.
	client, err := NewRegistryClient(ctx, []string{host}, ClientOptions{
		InsecureSkipTLSVerify: opts.InsecureSkipTLSVerify,
		ResponseHeaderTimeout: opts.ResponseHeaderTimeout,
		Credential:            StaticCredential(host, opts.Username, opts.Password),
		DialContext:           opts.DialContext,
	})
	if err != nil {
		return err
	}

	plainHTTP := opts.PlainHTTP
	if !plainHTTP {
		// Matches the pull path: try https, and fall back only when the registry does not
		// serve it. An internal mirror on plain HTTP is a normal thing to be pointed at.
		plainHTTP, err = ShouldUsePlainHTTP(ctx, host, client)
		if err != nil {
			return fmt.Errorf("unable to connect to the registry %s: %w", host, err)
		}
	}

	l.Info("pushing images", "count", len(imageList), "target", target)

	// One image at a time, with OCIConcurrency applied to the blobs inside each copy. This is
	// how the pull path spends its concurrency, and the reason to match it is that the two
	// settings compose: a group over images as well would put OCIConcurrency squared requests
	// in flight against a registry that was told what the limit was.
	for _, img := range imageList {
		if err := pushImage(ctx, src, img, target, client, plainHTTP, opts); err != nil {
			return fmt.Errorf("failed to push %s: %w", img.Reference, err)
		}
	}

	l.Info("done pushing images", "count", len(imageList), "target", target,
		"duration", time.Since(pushStart).Round(time.Millisecond*100))
	return nil
}

// pushImage copies one image out of the layout and into its repository under target.
func pushImage(ctx context.Context, src *oci.ReadOnlyStore, img transform.Image, target string, client *auth.Client, plainHTTP bool, opts PushOptions) error {
	l := logger.From(ctx)

	dstRef, err := transform.ImageTransformHostWithoutChecksum(target, img.Reference)
	if err != nil {
		return err
	}

	root, err := src.Resolve(ctx, img.Reference)
	if err != nil {
		return fmt.Errorf("the package does not hold %s: %w", img.Reference, err)
	}

	parsed, err := registry.ParseReference(dstRef)
	if err != nil {
		return fmt.Errorf("invalid destination reference %q: %w", dstRef, err)
	}
	repo := &orasRemote.Repository{Reference: parsed, PlainHTTP: plainHTTP, Client: client}

	size, err := graphSize(ctx, src, root)
	if err != nil {
		return err
	}
	l.Info("pushing image", "name", img.Reference, "to", dstRef, "size", utils.ByteFormat(float64(size), 2))

	copyOpts := oras.DefaultCopyOptions
	copyOpts.Concurrency = opts.OCIConcurrency

	return retry.Do(
		func() error {
			tracked := NewTrackedTarget(repo, size, DefaultReport(l, "image push in progress", dstRef))
			tracked.StartReporting(ctx)
			defer tracked.StopReporting()
			_, copyErr := oras.Copy(ctx, src, img.Reference, tracked, dstRef, copyOpts)
			return copyErr
		},
		retry.Attempts(uint(opts.Retries)),
		retry.Delay(config.ZarfDefaultRetryDelay),
		retry.MaxDelay(config.ZarfDefaultRetryMaxDelay),
		retry.DelayType(retry.BackOffDelay),
		retry.LastErrorOnly(true),
		retry.Context(ctx),
		retry.OnRetry(func(n uint, err error) {
			if opts.Retries > 1 && n+1 < uint(opts.Retries) {
				l.Warn("retrying image push", "attempt", n+1, "maxAttempts", opts.Retries, "image", dstRef, "error", err)
			}
		}),
	)
}

// graphSize totals the bytes reachable from root, so a push can report progress against a real
// figure rather than against the size of the manifest that describes the content.
//
// It walks the layout rather than reading a manifest's layer list directly, because root may be an
// image index -- which is what a multi-architecture package tags -- and the architectures under it
// share whatever blobs they have in common. Counting a shared blob once is also what makes the
// total match the bytes the copy actually moves.
func graphSize(ctx context.Context, src content.Fetcher, root ocispec.Descriptor) (int64, error) {
	seen := map[string]struct{}{}
	var walk func(ocispec.Descriptor) (int64, error)
	walk = func(desc ocispec.Descriptor) (int64, error) {
		key := desc.Digest.String()
		if _, ok := seen[key]; ok {
			return 0, nil
		}
		seen[key] = struct{}{}

		total := desc.Size
		children, err := content.Successors(ctx, src, desc)
		if err != nil {
			return 0, fmt.Errorf("unable to walk %s: %w", desc.Digest, err)
		}
		for _, child := range children {
			childSize, err := walk(child)
			if err != nil {
				return 0, err
			}
			total += childSize
		}
		return total, nil
	}
	return walk(root)
}
