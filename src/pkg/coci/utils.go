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
	"errors"
	"fmt"
	"strings"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/pkg/helpers"
	"oras.land/oras-go/v2/registry"
)

// ReferenceFromMetadata returns a reference for the given metadata.
func ReferenceFromMetadata(registryLocation string, pkg distro.ZarfDistro) (registry.Reference, error) {
	return ReferenceFromMetadataWithOptions(registryLocation, pkg, ReferenceFromMetadataOptions{})
}

// ReferenceFromMetadataOptions provides extensible options
type ReferenceFromMetadataOptions struct {
	// Tag specifies the OCI reference to use instead of package.metadata.version
	Tag string
}

// ReferenceFromMetadataWithOptions returns a reference for the given metadata with optional overrides
func ReferenceFromMetadataWithOptions(registryLocation string, pkg distro.ZarfDistro, opts ReferenceFromMetadataOptions) (registry.Reference, error) {
	// Explicit requirement for version in order to publish
	if len(pkg.Metadata.Version) == 0 {
		return registry.Reference{}, errors.New("version is required for publishing")
	}
	if !strings.HasSuffix(registryLocation, "/") {
		registryLocation = registryLocation + "/"
	}
	registryLocation = strings.TrimPrefix(registryLocation, helpers.OCIURLPrefix)

	// Use the explicit tag if provided
	// do not include flavor if provided
	tag := pkg.Metadata.Version
	if opts.Tag != "" {
		tag = opts.Tag
	}

	raw := fmt.Sprintf("%s%s:%s", registryLocation, pkg.Metadata.Name, tag)

	ref, err := registry.ParseReference(raw)
	if err != nil {
		return registry.Reference{}, fmt.Errorf("failed to parse %s: %w", raw, err)
	}
	return ref, nil
}
