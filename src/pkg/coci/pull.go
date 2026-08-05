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
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/pkg/coci/layers"
	"github.com/colonel-byte/cargoship/src/pkg/helpers"
	"github.com/defenseunicorns/pkg/oci"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/pkg/images"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/transform"
	"github.com/zarf-dev/zarf/src/pkg/utils"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/file"
)

var (
	// PackageAlwaysPull is a list of paths that will always be pulled from the remote repository.
	PackageAlwaysPull = []string{
		config.DistroYAML,
		config.Checksums,
		config.Bundle,
	}
)

// PullDistro pulls the package from the remote repository and saves it to the given path.
func (r *Remote) PullDistro(ctx context.Context, destinationDir string, concurrency int, layersToPull ...ocispec.Descriptor) (_ []ocispec.Descriptor, err error) {
	start := time.Now()
	// layersToPull is an explicit requirement for pulling package layers
	if len(layersToPull) == 0 {
		return nil, fmt.Errorf("no layers to pull")
	}

	if concurrency == 0 {
		concurrency = DefaultConcurrency
	}

	layerSize := oci.SumDescsSize(layersToPull)
	logger.From(ctx).Info("pulling package", "name", r.Repo().Reference, "size", utils.ByteFormat(float64(layerSize), 2))

	dst, err := file.New(destinationDir)
	if err != nil {
		return nil, err
	}
	defer func(dst *file.Store) {
		err2 := dst.Close()
		err = errors.Join(err, err2)
	}(dst)

	copyOpts := r.GetDefaultCopyOpts()
	copyOpts.Concurrency = concurrency

	trackedDst := images.NewTrackedTarget(dst, layerSize, images.DefaultReport(r.Log(), "package pull in progress", r.Repo().Reference.String()))
	trackedDst.StartReporting(ctx)
	defer trackedDst.StopReporting()

	err = r.CopyToTarget(ctx, layersToPull, trackedDst, copyOpts)
	if err != nil {
		return nil, err
	}
	r.Log().Info("finished pulling package layers", "duration", time.Since(start).Round(time.Millisecond*100))
	return layersToPull, nil
}

// AssembleLayers returns the OCI layer descriptors for the requested components.
// The include parameter specifies which layer types to return.
// All layers are included if include is empty and Metadata layers are always included
func AssembleLayers(ctx context.Context, root *oci.Manifest, fetcher content.Fetcher, include ...layers.LayerType) ([]ocispec.Descriptor, error) {
	if len(include) == 0 {
		include = layers.GetAllLayerTypes() //nolint:ineffassign,staticcheck
	}

	// Metadata layers are always included
	layers := make([]ocispec.Descriptor, 0)
	for _, path := range PackageAlwaysPull {
		desc := root.Locate(path)
		if !oci.IsEmptyDescriptor(desc) {
			layers = append(layers, desc)
		}
	}

	dis, err := FetchDistroYAML(ctx, root, fetcher)
	if err != nil {
		return nil, err
	}

	if len(dis.Spec.Config.ImagesConfig.Images) > 0 {
		imageLayers, err := LayersFromImages(ctx, root, fetcher, dis.Spec.Config.ImagesConfig.Images)
		if err != nil {
			return nil, err
		}
		layers = append(layers, imageLayers...)
	}

	if len(dis.Spec.Config.Files) > 0 {
		fileLayers, err := LayersFromFiles(ctx, root, dis.Spec.Config.Files, config.FilesDir)
		if err != nil {
			return nil, err
		}
		layers = append(layers, fileLayers...)
	}

	if len(dis.Spec.Config.OS.Files) > 0 {
		fileLayers, err := LayersFromFiles(ctx, root, dis.Spec.Config.OS.Files, config.OSDir)
		if err != nil {
			return nil, err
		}
		layers = append(layers, fileLayers...)
	}

	return oci.RemoveDuplicateDescriptors(layers), nil
}

// AssembleLayers returns the OCI layer descriptors for the requested components.
// The include parameter specifies which layer types to return.
// All layers are included if include is empty and Metadata layers are always included
func (r *Remote) AssembleLayers(ctx context.Context, include ...layers.LayerType) ([]ocispec.Descriptor, error) {
	root, err := r.FetchRoot(ctx)
	if err != nil {
		return nil, err
	}
	return AssembleLayers(ctx, root, r, include...)
}

// LayersFromFiles returns the file oci blob layers referenced by annotation.
func LayersFromFiles(ctx context.Context, root *oci.Manifest, files v1alpha1.ZarfFiles, folder string) ([]ocispec.Descriptor, error) {
	layers := make([]ocispec.Descriptor, 0)

	for i, f := range files {
		path := filepath.Join(folder, strconv.Itoa(i), filepath.Base(f.Target))
		desc := root.Locate(path)
		if !oci.IsEmptyDescriptor(desc) {
			logger.From(ctx).Debug("found a file", "desc", desc.Digest.Encoded()[:12], "path", path)
			layers = append(layers, desc)
		}
	}

	// Remove duplicate descriptors in case of shared base layers
	return oci.RemoveDuplicateDescriptors(layers), nil
}

// LayersFromImages returns the image blob layers referenced by imageList, selecting
// from root. fetcher reads image manifests/indexes to walk multi-arch children.
func LayersFromImages(ctx context.Context, root *oci.Manifest, fetcher content.Fetcher, imageList []string) ([]ocispec.Descriptor, error) {
	index, err := oci.FetchJSONFile[*ocispec.Index](ctx, fetcher, root, config.IndexPath)
	if err != nil {
		return nil, err
	}

	layers := make([]ocispec.Descriptor, 0)
	layers = append(layers, root.Locate(config.IndexPath), root.Locate(config.OCILayoutPath))

	for _, image := range imageList {
		// use docker's transform lib to parse the image ref
		// this properly mirrors the logic within create
		refInfo, err := transform.ParseImageRef(image)
		if err != nil {
			return nil, fmt.Errorf("failed to parse image ref %q: %w", image, err)
		}

		entry := helpers.Find(index.Manifests, func(layer ocispec.Descriptor) bool {
			return layer.Annotations[ocispec.AnnotationBaseImageName] == refInfo.Reference || (layer.Annotations[ocispec.AnnotationBaseImageName] == refInfo.Path+refInfo.TagOrDigest && refInfo.Host == "docker.io")
		})

		if entry.Digest == "" {
			return nil, fmt.Errorf("image %q not found in package index", refInfo.Reference)
		}

		layers = append(layers, root.Locate(filepath.Join(config.ImagesBlobsDir, entry.Digest.Encoded())))

		switch {
		case images.IsIndex(entry.MediaType):
			childLayers, err := layersFromIndexChildren(ctx, root, fetcher, entry)
			if err != nil {
				return nil, err
			}
			layers = append(layers, childLayers...)
		case images.IsManifest(entry.MediaType):
			manifestLayers, err := layersFromManifestChildren(ctx, root, fetcher, entry)
			if err != nil {
				return nil, err
			}
			layers = append(layers, manifestLayers...)
		default:
			return nil, fmt.Errorf("unexpected media type %q for image %s", entry.MediaType, entry.Digest)
		}
	}
	// Remove duplicate descriptors in case of shared base layers
	return oci.RemoveDuplicateDescriptors(layers), nil
}

func layersFromManifestChildren(ctx context.Context, root *oci.Manifest, fetcher content.Fetcher, manifestDesc ocispec.Descriptor) ([]ocispec.Descriptor, error) {
	manifest, err := oci.FetchJSONFile[*ocispec.Manifest](ctx, fetcher, root, filepath.Join(config.ImagesBlobsDir, manifestDesc.Digest.Encoded()))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifest %s: %w", manifestDesc.Digest, err)
	}
	layers := make([]ocispec.Descriptor, 0, len(manifest.Layers)+1)
	if manifest.Config.Digest != "" {
		layers = append(layers, root.Locate(filepath.Join(config.ImagesBlobsDir, manifest.Config.Digest.Encoded())))
	}
	for _, layer := range manifest.Layers {
		layers = append(layers, root.Locate(filepath.Join(config.ImagesBlobsDir, layer.Digest.Encoded())))
	}
	return layers, nil
}

func layersFromIndexChildren(ctx context.Context, root *oci.Manifest, fetcher content.Fetcher, indexDesc ocispec.Descriptor) ([]ocispec.Descriptor, error) {
	idx, err := oci.FetchJSONFile[*ocispec.Index](ctx, fetcher, root, filepath.Join(config.ImagesBlobsDir, indexDesc.Digest.Encoded()))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch child index %s: %w", indexDesc.Digest, err)
	}
	layers := make([]ocispec.Descriptor, 0, len(idx.Manifests))
	for _, child := range idx.Manifests {
		layers = append(layers, root.Locate(filepath.Join(config.ImagesBlobsDir, child.Digest.Encoded())))
		switch {
		case images.IsIndex(child.MediaType):
			nestedLayers, err := layersFromIndexChildren(ctx, root, fetcher, child)
			if err != nil {
				return nil, err
			}
			layers = append(layers, nestedLayers...)
		case images.IsManifest(child.MediaType):
			manifestLayers, err := layersFromManifestChildren(ctx, root, fetcher, child)
			if err != nil {
				return nil, err
			}
			layers = append(layers, manifestLayers...)
		default:
			return nil, fmt.Errorf("unexpected media type %q for index child %s", child.MediaType, child.Digest)
		}
	}
	return layers, nil
}
