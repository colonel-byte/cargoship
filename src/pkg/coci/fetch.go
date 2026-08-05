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

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/internal/cfg"
	"github.com/defenseunicorns/pkg/oci"
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
