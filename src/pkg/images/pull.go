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
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/zarf-dev/zarf/src/config"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/utils"
	"golang.org/x/sync/errgroup"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/registry"

	"github.com/colonel-byte/cargoship/src/internal/dns"
	"github.com/colonel-byte/cargoship/src/pkg/helpers"
	"github.com/colonel-byte/cargoship/src/pkg/oci/cache"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/pkg/transform"
	orasRemote "oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
)

// PullOptions is the configuration for pulling images.
type PullOptions struct {
	OCIConcurrency        int
	Arch                  string
	RegistryOverrides     []RegistryOverride
	CacheDirectory        string
	PlainHTTP             bool
	InsecureSkipTLSVerify bool
	ResponseHeaderTimeout time.Duration
}

type imagePullInfo struct {
	registryOverrideRef string
	ref                 string
	manifestDesc        ocispec.Descriptor
	byteSize            int64
}

type imageWithOverride struct {
	overridden transform.Image
	original   transform.Image
}

// Pull pulls all images to the destination directory.
func Pull(ctx context.Context, imageList []transform.Image, destinationDirectory string, opts PullOptions) ([]ImageWithManifest, error) {
	if len(imageList) == 0 {
		return nil, fmt.Errorf("image list is required")
	}
	if destinationDirectory == "" {
		return nil, fmt.Errorf("destination directory is required")
	}
	imageList = helpers.Unique(imageList)
	l := logger.From(ctx)
	pullStart := time.Now()

	imageCount := len(imageList)
	if err := helpers.CreateDirectory(destinationDirectory, helpers.ReadExecuteAllWriteUser); err != nil {
		return nil, fmt.Errorf("failed to create image path %s: %w", destinationDirectory, err)
	}

	if err := helpers.CreateDirectory(opts.CacheDirectory, helpers.ReadExecuteAllWriteUser); err != nil {
		return nil, fmt.Errorf("failed to create cache directory %s: %w", destinationDirectory, err)
	}

	if opts.ResponseHeaderTimeout < 0 {
		opts.ResponseHeaderTimeout = 0 // currently allowing infinite timeout
	}

	imagesWithOverride := []imageWithOverride{}
	// Iterate over all images, marking each one as overridden.
	for _, img := range imageList {
		overriddenImage := img
		for _, v := range opts.RegistryOverrides {
			if strings.HasPrefix(img.Reference, v.Source) {
				// If we have an override, the first override wins.
				// Doing so allows earlier, longer prefixes (such as docker.io/library)
				// to supersede shorter prefixes (such as docker.io).
				overriddenImage.Reference = strings.Replace(img.Reference, v.Source, v.Override, 1)
				break
			}
		}
		imagesWithOverride = append(imagesWithOverride, imageWithOverride{
			original:   img,
			overridden: overriddenImage,
		})
	}

	imageFetchStart := time.Now()
	l.Info("fetching info for images", "count", imageCount, "destination", destinationDirectory)
	storeOpts := credentials.StoreOptions{}
	credStore, err := credentials.NewStoreFromDocker(storeOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials: %w", err)
	}
	transport, err := orasTransport(opts.InsecureSkipTLSVerify, opts.ResponseHeaderTimeout)
	if err != nil {
		return nil, err
	}
	client := &auth.Client{
		Client: &http.Client{
			Transport: transport,
		},
		Cache:      auth.NewCache(),
		Credential: credentials.Credential(credStore),
	}
	uniqueHosts := map[string]struct{}{}
	for _, v := range imagesWithOverride {
		uniqueHosts[v.overridden.Host] = struct{}{}
	}
	// We ping registries to pre-authenticate as some auth mechanisms open up a browser.
	// When this happens concurrently a browser tab is opened for each image from that host and authenticating to one tab will not propagate creds
	// Instead we auth synchronously with ping so the auth is cached before concurrent fetch.
	if credStore.IsAuthConfigured() {
		for host := range uniqueHosts {
			registry, err := orasRemote.NewRegistry(host)
			if err != nil {
				return nil, fmt.Errorf("failed to create registry: %w", err)
			}
			registry.Client = client
			// we can't error here because there may be a faked registry used for the docker fallback mechanism
			_ = registry.Ping(ctx) //nolint: errcheck
		}
	}

	l.Debug("gathering credentials from default Docker config file", "credentialsConfigured", credStore.IsAuthConfigured())
	platform := &ocispec.Platform{
		Architecture: opts.Arch,
		// TODO: in the future we could support Windows images
		OS: "linux",
	}
	imagesWithManifests := []ImageWithManifest{}
	imagesInfo := []imagePullInfo{}
	dockerFallBackImages := []imageWithOverride{}
	var imageListLock sync.Mutex

	// This loop pulls the metadata from images with three goals
	// - Get all the manifests from images that will be pulled so they can be returned to the function
	// - discover if any images are sha'd to an index, if so error and inform user on the different available platforms
	// - Mark any images that don't resolve so we can attempt to pull them from the daemon
	eg, ectx := errgroup.WithContext(ctx)
	eg.SetLimit(10)
	for _, image := range imagesWithOverride {
		eg.Go(func() error {
			repo := &orasRemote.Repository{}

			ref, err := registry.ParseReference(image.overridden.Reference)
			if err != nil {
				return err
			}
			repo.Reference = ref
			repo.Client = client

			repo.PlainHTTP = opts.PlainHTTP
			if dns.IsLocalhost(repo.Reference.Host()) && !opts.PlainHTTP {
				repo.PlainHTTP, err = ShouldUsePlainHTTP(ctx, repo.Reference.Host(), client)
				// If the pings to localhost fail, it could be an image on the daemon
				if err != nil {
					l.Warn("unable to authenticate to host, attempting pull from docker daemon as fallback", "image", image.overridden.Reference, "err", err)
					imageListLock.Lock()
					defer imageListLock.Unlock()
					dockerFallBackImages = append(dockerFallBackImages, image)
					return nil
				}
			}

			fetchOpts := oras.DefaultFetchBytesOptions
			desc, b, err := oras.FetchBytes(ectx, repo, image.overridden.Reference, fetchOpts)
			if err != nil {
				// TODO we could use the k8s library for backoffs here - https://github.com/kubernetes/kubernetes/blob/master/staging/src/k8s.io/apimachinery/pkg/util/wait/backoff.go
				if strings.Contains(err.Error(), "toomanyrequests") {
					return fmt.Errorf("rate limited by registry: %w", err)
				}
				l.Warn("unable to find image, attempting pull from docker daemon as fallback", "image", image.overridden.Reference, "err", err)
				imageListLock.Lock()
				defer imageListLock.Unlock()
				dockerFallBackImages = append(dockerFallBackImages, image)
				return nil
			}

			// If the image sha points to an index then error
			if image.original.Digest != "" && isIndex(desc.MediaType) {
				// Both index types can be marshalled into an ocispec.Index
				// https://github.com/oras-project/oras-go/blob/853e0125ccad32ff691e4ed70e156c7619021bfd/internal/manifestutil/parser.go#L55
				var idx ocispec.Index
				if err := json.Unmarshal(b, &idx); err != nil {
					return fmt.Errorf("unable to unmarshal index.json: %w", err)
				}
				return constructIndexError(idx, image.overridden)
			}
			// If a manifest was returned from FetchBytes, either it's a tag with only one image or it's a non container image
			// If it's not a manifest then we received an index and need to pull the manifest by platform
			if !isManifest(desc.MediaType) {
				fetchOpts.FetchOptions.TargetPlatform = platform
				desc, b, err = oras.FetchBytes(ectx, repo, image.overridden.Reference, fetchOpts)
				if err != nil {
					return fmt.Errorf("failed to fetch image %s with architecture %s: %w", image.overridden.Reference, platform.Architecture, err)
				}
			}

			// extra validation before we marshall, this should never be true
			if !isManifest(desc.MediaType) {
				return fmt.Errorf("received unexpected mediatype %s", desc.MediaType)
			}
			// Both oci and docker manifest types can be marshalled into a manifest
			// https://github.com/oras-project/oras-go/blob/853e0125ccad32ff691e4ed70e156c7619021bfd/internal/manifestutil/parser.go#L37
			var manifest ocispec.Manifest
			if err := json.Unmarshal(b, &manifest); err != nil {
				return err
			}
			size := getSizeOfImage(desc, manifest)
			imageListLock.Lock()
			defer imageListLock.Unlock()
			imagesInfo = append(imagesInfo, imagePullInfo{
				registryOverrideRef: image.overridden.Reference,
				ref:                 image.original.Reference,
				byteSize:            size,
				manifestDesc:        desc,
			})
			imagesWithManifests = append(imagesWithManifests, ImageWithManifest{
				Image:    image.original,
				Manifest: manifest,
			})
			l.Debug("pulled manifest for image", "name", image.overridden.Reference)
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	l.Debug("done fetching info for images", "count", imageCount, "duration", time.Since(imageFetchStart))

	l.Info("pulling images", "count", imageCount)

	dst, err := oci.NewWithContext(ctx, destinationDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to create oci layout: %w", err)
	}

	for _, imageInfo := range imagesInfo {
		err = orasSave(ctx, imageInfo, opts, dst, client)
		if err != nil {
			return nil, fmt.Errorf("failed to save images: %w", err)
		}
	}

	l.Info("done pulling images", "count", imageCount, "duration", time.Since(pullStart).Round(time.Millisecond*100))

	return imagesWithManifests, nil
}

func constructIndexError(idx ocispec.Index, image transform.Image) error {
	lines := []string{"The following images are available in the index:"}
	name := image.Name
	if image.Tag != "" {
		name += ":" + image.Tag
	}
	for _, desc := range idx.Manifests {
		lines = append(lines, fmt.Sprintf("image - %s@%s with platform %s", name, desc.Digest, desc.Platform))
	}
	imageOptions := strings.Join(lines, "\n")
	return fmt.Errorf("%s resolved to an OCI image index which is not supported by Zarf, select a specific platform to use: %s", image.Reference, imageOptions)
}

func orasSave(ctx context.Context, imageInfo imagePullInfo, opts PullOptions, dst *oci.Store, client *auth.Client) error {
	l := logger.From(ctx)
	var pullSrc oras.ReadOnlyTarget
	var err error
	repo := &orasRemote.Repository{}
	repo.Reference, err = registry.ParseReference(imageInfo.registryOverrideRef)
	if err != nil {
		return fmt.Errorf("failed to parse image reference %s: %w", imageInfo.registryOverrideRef, err)
	}
	repo.PlainHTTP = opts.PlainHTTP
	if dns.IsLocalhost(repo.Reference.Host()) && !opts.PlainHTTP {
		repo.PlainHTTP, err = ShouldUsePlainHTTP(ctx, repo.Reference.Host(), client)
		if err != nil {
			return fmt.Errorf("unable to connect to the registry %s: %w", repo.Reference.Host(), err)
		}
	}
	repo.Client = client

	copyOpts := oras.DefaultCopyOptions
	copyOpts.Concurrency = opts.OCIConcurrency
	copyOpts.WithTargetPlatform(imageInfo.manifestDesc.Platform)
	l.Info("saving image", "name", imageInfo.registryOverrideRef, "size", utils.ByteFormat(float64(imageInfo.byteSize), 2))
	localCache, err := oci.NewWithContext(ctx, opts.CacheDirectory)
	if err != nil {
		return fmt.Errorf("failed to create oci formatted directory: %w", err)
	}
	pullSrc = cache.New(repo, localCache)
	var desc ocispec.Descriptor
	err = retry.Do(
		func() error {
			trackedDst := NewTrackedTarget(dst, imageInfo.byteSize, DefaultReport(l, "image pull in progress", imageInfo.registryOverrideRef))
			trackedDst.StartReporting(ctx)
			defer trackedDst.StopReporting()
			var copyErr error
			desc, copyErr = oras.Copy(ctx, pullSrc, imageInfo.registryOverrideRef, trackedDst, imageInfo.ref, copyOpts)
			return copyErr
		},
		retry.Attempts(uint(config.ZarfDefaultRetries)),
		retry.Delay(config.ZarfDefaultRetryDelay),
		retry.MaxDelay(config.ZarfDefaultRetryMaxDelay),
		retry.DelayType(retry.BackOffDelay),
		retry.LastErrorOnly(true),
		retry.Context(ctx),
		retry.OnRetry(func(n uint, err error) {
			if config.ZarfDefaultRetries > 1 && n+1 < uint(config.ZarfDefaultRetries) {
				l.Warn("retrying image pull",
					"attempt", n+1,
					"maxAttempts", config.ZarfDefaultRetries,
					"image", imageInfo.registryOverrideRef,
					"error", err,
				)
			}
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to copy: %w", err)
	}
	desc = addNameAnnotationsToDesc(desc, imageInfo.ref)
	err = dst.Tag(ctx, desc, imageInfo.ref)
	if err != nil {
		return fmt.Errorf("failed to tag image: %w", err)
	}
	return nil
}
