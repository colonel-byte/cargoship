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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/zarf-dev/zarf/src/config"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/utils"
	"golang.org/x/sync/errgroup"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry"

	"github.com/colonel-byte/cargoship/src/internal/dns"
	"github.com/colonel-byte/cargoship/src/pkg/helpers"
	orasCache "github.com/defenseunicorns/pkg/oci/cache"
	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/pkg/transform"
	orasRemote "oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
)

// PullOptions is the configuration for pulling images.
type PullOptions struct {
	OCIConcurrency int
	// Arches lists the CPU architectures to pull each image for. An image pulled for more than one
	// architecture is tagged as an OCI image index, so the architectures share whatever layers they
	// have in common instead of being stored twice.
	Arches                []string
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

// fetchedManifest is a manifest descriptor and the bytes it points at, as returned by a metadata
// fetch. An image pulled for several architectures produces one of these per architecture.
type fetchedManifest struct {
	desc ocispec.Descriptor
	body []byte
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
		return nil, fmt.Errorf("failed to create cache directory %s: %w", opts.CacheDirectory, err)
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
	pullPlatforms := platformsForArches(opts.Arches)
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
			// If a manifest was returned from FetchBytes, either it's a tag with only one image or it's a
			// non container image, and that single manifest is what every requested architecture gets.
			// If it's not a manifest then we received an index and need to pull one manifest per platform.
			fetched := []fetchedManifest{{desc: desc, body: b}}
			if !isManifest(desc.MediaType) {
				fetched = fetched[:0]
				for _, pullPlatform := range pullPlatforms {
					platformOpts := oras.DefaultFetchBytesOptions
					platformOpts.FetchOptions.TargetPlatform = &pullPlatform
					platformDesc, platformBody, err := oras.FetchBytes(ectx, repo, image.overridden.Reference, platformOpts)
					if err != nil {
						return fmt.Errorf("failed to fetch image %s with architecture %s: %w", image.overridden.Reference, pullPlatform.Architecture, err)
					}
					fetched = append(fetched, fetchedManifest{desc: platformDesc, body: platformBody})
				}
			}

			imageListLock.Lock()
			defer imageListLock.Unlock()
			seen := map[digest.Digest]struct{}{}
			for _, f := range fetched {
				// extra validation before we marshall, this should never be true
				if !isManifest(f.desc.MediaType) {
					return fmt.Errorf("received unexpected mediatype %s", f.desc.MediaType)
				}
				// An index is allowed to point several platforms at the same manifest, and there is no
				// reason to copy that manifest more than once.
				if _, ok := seen[f.desc.Digest]; ok {
					continue
				}
				seen[f.desc.Digest] = struct{}{}
				// Both oci and docker manifest types can be marshalled into a manifest
				// https://github.com/oras-project/oras-go/blob/853e0125ccad32ff691e4ed70e156c7619021bfd/internal/manifestutil/parser.go#L37
				var manifest ocispec.Manifest
				if err := json.Unmarshal(f.body, &manifest); err != nil {
					return err
				}
				imagesInfo = append(imagesInfo, imagePullInfo{
					registryOverrideRef: image.overridden.Reference,
					ref:                 image.original.Reference,
					byteSize:            getSizeOfImage(f.desc, manifest),
					manifestDesc:        f.desc,
				})
				imagesWithManifests = append(imagesWithManifests, ImageWithManifest{
					Image:    image.original,
					Manifest: manifest,
				})
			}
			l.Debug("pulled manifests for image", "name", image.overridden.Reference, "count", len(fetched))
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

	// Tagging happens after every manifest is in the store, because an image pulled for several
	// architectures is tagged once, as an index over the manifests saved for it.
	tagOrder := []string{}
	savedDescs := map[string][]ocispec.Descriptor{}
	for _, imageInfo := range imagesInfo {
		desc, err := orasSave(ctx, imageInfo, opts, dst, client)
		if err != nil {
			return nil, fmt.Errorf("failed to save images: %w", err)
		}
		if _, ok := savedDescs[imageInfo.ref]; !ok {
			tagOrder = append(tagOrder, imageInfo.ref)
		}
		savedDescs[imageInfo.ref] = append(savedDescs[imageInfo.ref], desc)
	}

	for _, ref := range tagOrder {
		if err := tagImage(ctx, dst, ref, savedDescs[ref]); err != nil {
			return nil, err
		}
	}

	if err := sortIndexFile(filepath.Join(destinationDirectory, ocispec.ImageIndexFile)); err != nil {
		return nil, err
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

// orasSave copies one image manifest and its blobs into dst and returns the manifest descriptor.
// It does not tag what it copied; see tagImage.
func orasSave(ctx context.Context, imageInfo imagePullInfo, opts PullOptions, dst *oci.Store, client *auth.Client) (ocispec.Descriptor, error) {
	l := logger.From(ctx)
	var pullSrc oras.ReadOnlyTarget
	var err error
	repo := &orasRemote.Repository{}
	repo.Reference, err = registry.ParseReference(imageInfo.registryOverrideRef)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("failed to parse image reference %s: %w", imageInfo.registryOverrideRef, err)
	}
	repo.PlainHTTP = opts.PlainHTTP
	if dns.IsLocalhost(repo.Reference.Host()) && !opts.PlainHTTP {
		repo.PlainHTTP, err = ShouldUsePlainHTTP(ctx, repo.Reference.Host(), client)
		if err != nil {
			return ocispec.Descriptor{}, fmt.Errorf("unable to connect to the registry %s: %w", repo.Reference.Host(), err)
		}
	}
	repo.Client = client

	copyOpts := oras.DefaultCopyOptions
	copyOpts.Concurrency = opts.OCIConcurrency
	copyOpts.WithTargetPlatform(imageInfo.manifestDesc.Platform)
	// A multi architecture pull saves one manifest per architecture under the same reference, so the
	// architecture is what tells two otherwise identical lines apart. A bare manifest from a registry
	// carries no platform, and that is the single architecture case where there is nothing to tell
	// apart, so the field is left off rather than reported as unknown.
	saveArgs := []any{"name", imageInfo.registryOverrideRef}
	if imageInfo.manifestDesc.Platform != nil && imageInfo.manifestDesc.Platform.Architecture != "" {
		saveArgs = append(saveArgs, "architecture", imageInfo.manifestDesc.Platform.Architecture)
	}
	saveArgs = append(saveArgs, "size", utils.ByteFormat(float64(imageInfo.byteSize), 2))
	l.Info("saving image", saveArgs...)
	localCache, err := oci.NewWithContext(ctx, opts.CacheDirectory)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("failed to create oci formatted directory: %w", err)
	}
	pullSrc = orasCache.New(repo, localCache)
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
		return ocispec.Descriptor{}, fmt.Errorf("failed to copy: %w", err)
	}
	return desc, nil
}

// platformsForArches returns the platforms to fetch each image for. An empty architecture list keeps
// the single unset platform this used to build, so a caller that never asked for an architecture
// behaves as it did before.
func platformsForArches(arches []string) []ocispec.Platform {
	if len(arches) == 0 {
		// TODO: in the future we could support Windows images
		return []ocispec.Platform{{OS: "linux"}}
	}

	pullPlatforms := make([]ocispec.Platform, 0, len(arches))
	for _, arch := range arches {
		pullPlatforms = append(pullPlatforms, ocispec.Platform{
			Architecture: arch,
			// TODO: in the future we could support Windows images
			OS: "linux",
		})
	}
	return pullPlatforms
}

// tagImage points ref at the content orasSave copied into the store. One manifest is tagged
// directly, which is what a package targeting a single architecture has always contained. Several
// manifests are gathered under an OCI image index and the index is tagged instead, so one reference
// resolves to every architecture the package targets while the architectures share whatever blobs
// they have in common.
func tagImage(ctx context.Context, dst *oci.Store, ref string, descs []ocispec.Descriptor) error {
	switch len(descs) {
	case 0:
		return fmt.Errorf("no manifests were saved for image %s", ref)
	case 1:
		if err := dst.Tag(ctx, addNameAnnotationsToDesc(descs[0], ref), ref); err != nil {
			return fmt.Errorf("failed to tag image: %w", err)
		}
		return nil
	}

	manifests := make([]ocispec.Descriptor, 0, len(descs))
	for _, desc := range descs {
		withPlatform, err := ensurePlatform(ctx, dst, desc)
		if err != nil {
			return fmt.Errorf("failed to read the platform of %s in %s: %w", desc.Digest, ref, err)
		}
		manifests = append(manifests, withPlatform)
	}
	// The index is part of the package and a package has to be reproducible, so the manifests are
	// ordered by platform rather than by whichever pull happened to finish first.
	sort.SliceStable(manifests, func(i, j int) bool {
		return platformKey(manifests[i].Platform) < platformKey(manifests[j].Platform)
	})

	b, err := json.Marshal(ocispec.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: manifests,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal the image index for %s: %w", ref, err)
	}
	indexDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageIndex,
		Digest:    digest.FromBytes(b),
		Size:      int64(len(b)),
	}
	if err := dst.Push(ctx, indexDesc, bytes.NewReader(b)); err != nil && !errors.Is(err, errdef.ErrAlreadyExists) {
		return fmt.Errorf("failed to push the image index for %s: %w", ref, err)
	}
	if err := dst.Tag(ctx, addNameAnnotationsToDesc(indexDesc, ref), ref); err != nil {
		return fmt.Errorf("failed to tag image index: %w", err)
	}
	return nil
}

// ensurePlatform fills in a manifest descriptor's platform. A descriptor resolved through a registry
// index already carries one. A registry that served a bare manifest does not, so the platform is
// read out of the image config, which is where the registry would have read it from as well.
func ensurePlatform(ctx context.Context, src content.Fetcher, desc ocispec.Descriptor) (ocispec.Descriptor, error) {
	if desc.Platform != nil && desc.Platform.Architecture != "" {
		return desc, nil
	}

	manifestBytes, err := content.FetchAll(ctx, src, desc)
	if err != nil {
		return desc, err
	}
	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return desc, err
	}
	configBytes, err := content.FetchAll(ctx, src, manifest.Config)
	if err != nil {
		return desc, err
	}
	var image ocispec.Image
	if err := json.Unmarshal(configBytes, &image); err != nil {
		return desc, err
	}
	if image.Architecture == "" {
		return desc, fmt.Errorf("image config %s does not name an architecture", manifest.Config.Digest)
	}

	platform := image.Platform
	desc.Platform = &platform
	return desc, nil
}

// sortIndexFile rewrites the store's index.json with its entries ordered by digest. Oras builds
// that list by ranging over a map, so two runs that pull exactly the same images write the same
// entries in a different order. The file ships inside the package and is covered by checksums.txt,
// so the order has to be pinned for a package to be reproducible. Entry order carries no meaning to
// a reader of the layout, which is why this can be done for every build rather than only for
// --reproducible.
func sortIndexFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to read the image index: %w", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read the image index: %w", err)
	}
	var index ocispec.Index
	if err := json.Unmarshal(b, &index); err != nil {
		return fmt.Errorf("failed to unmarshal the image index: %w", err)
	}

	// Two entries share a digest when one image is tagged under more than one reference, so the
	// reference name breaks the tie.
	sort.SliceStable(index.Manifests, func(i, j int) bool {
		if index.Manifests[i].Digest != index.Manifests[j].Digest {
			return index.Manifests[i].Digest < index.Manifests[j].Digest
		}
		return index.Manifests[i].Annotations[ocispec.AnnotationRefName] <
			index.Manifests[j].Annotations[ocispec.AnnotationRefName]
	})

	sorted, err := json.Marshal(index)
	if err != nil {
		return fmt.Errorf("failed to marshal the image index: %w", err)
	}
	if err := os.WriteFile(path, sorted, info.Mode().Perm()); err != nil {
		return fmt.Errorf("failed to write the image index: %w", err)
	}
	return nil
}

// platformKey renders a platform as the os/arch/variant string used to order an index.
func platformKey(platform *ocispec.Platform) string {
	if platform == nil {
		return ""
	}
	return strings.Join([]string{platform.OS, platform.Architecture, platform.Variant}, "/")
}
