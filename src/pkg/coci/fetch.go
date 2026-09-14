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
	"encoding/json"
	"fmt"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/internal/cfg"
	"github.com/defenseunicorns/pkg/oci"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
)

// FetchDistroYAML fetches the distro.yaml file from the remote repository.
func FetchDistroYAML(ctx context.Context, root *oci.Manifest, fetcher content.Fetcher) (distro.ZarfDistro, error) {
	descriptor := root.Locate(config.DistroYAML)
	if oci.IsEmptyDescriptor(descriptor) {
		return distro.ZarfDistro{}, fmt.Errorf("unable to find %s in the manifest", config.DistroYAML)
	}
	b, err := content.FetchAll(ctx, fetcher, descriptor)
	if err != nil {
		return distro.ZarfDistro{}, err
	}
	return cfg.ParseMultiDoc(ctx, b)
}

// FetchDistroYAML fetches the distro.yaml file from the remote repository.
func (r *Remote) FetchDistroYAML(ctx context.Context) (distro.ZarfDistro, error) {
	root, err := r.FetchRoot(ctx)
	if err != nil {
		return distro.ZarfDistro{}, err
	}
	return FetchDistroYAML(ctx, root, r)
}

// FetchImagesIndex fetches the images/index.json file from the remote repository.
func (r *Remote) FetchImagesIndex(ctx context.Context) (*ocispec.Index, error) {
	manifest, err := r.FetchRoot(ctx)
	if err != nil {
		return nil, err
	}
	result, err := oci.FetchJSONFile[*ocispec.Index](ctx, r, manifest, config.IndexPath)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// SoleManifestDigest returns the digest of the only package manifest the index at the remote's
// reference lists. A package published for one architecture holds a single manifest, so there is
// nothing to choose between and the caller can pull it whatever platform the index records it
// under. It errors when the index lists more than one distinct manifest, since picking between
// those is what platform matching is for.
func (r *Remote) SoleManifestDigest(ctx context.Context) (digest.Digest, error) {
	ref := r.Repo().Reference.Reference

	desc, rc, err := r.Repo().FetchReference(ctx, ref)
	if err != nil {
		return "", err
	}
	defer rc.Close() //nolint:errcheck // read-only body, nothing to do with a close error

	if desc.MediaType != ocispec.MediaTypeImageIndex {
		return "", fmt.Errorf("%s is not an image index", ref)
	}

	b, err := content.ReadAll(rc, desc)
	if err != nil {
		return "", err
	}
	var index ocispec.Index
	if err := json.Unmarshal(b, &index); err != nil {
		return "", err
	}

	distinct := make(map[digest.Digest]struct{}, len(index.Manifests))
	for _, manifest := range index.Manifests {
		distinct[manifest.Digest] = struct{}{}
	}
	if len(distinct) != 1 {
		return "", fmt.Errorf("index %s lists %d distinct manifests, none of which match the requested platform", ref, len(distinct))
	}

	return index.Manifests[0].Digest, nil
}
