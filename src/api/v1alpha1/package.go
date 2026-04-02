// Copyright 2026 colonel-byte
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

package v1alpha1

import (
	zarf "github.com/zarf-dev/zarf/src/api/v1alpha1"
)

type ZarfDistroPackage struct {
	APIVersion string                    `json:"apiVersion,omitempty" jsonschema:"enum=zarf.dev/v1alpha1"`
	Kind       ZarfDistroKind            `json:"kind" jsonschema:"enum=ZarfDistroPackage"`
	Metadata   ZarfDistroPackageMetadata `json:"metadata"`
	Build      ZarfDistroBuildData       `json:"build,omitempty"`
	Spec       ZarfDistroPackageSpec     `json:"spec"`
}

type ZarfDistroPackageMetadata struct {
	Uncompressed bool   `json:"uncompressed,omitempty" jsonschema:"default=false"`
	Architecture string `json:"architecture,omitempty" jsonschema:"default=amd64,enum=amd64,enum=arm64"`
	ZarfDistroMetadata
}

type ZarfDistroBuildData struct {
	Architecture      string            `json:"architecture,omitempty"`
	Timestamp         string            `json:"timestamp,omitempty"`
	Version           string            `json:"version,omitempty"`
	RegistryOverrides map[string]string `json:"registryOverrides,omitempty"`
}

type ZarfDistroPackageSpec struct {
	Actions ZarfDistroActions       `json:"actions"`
	Distro  ZarfDistroPackageConfig `json:"distro"`
}

type ZarfDistroActions struct {
	OnCreate  zarf.ZarfComponentActionSet `json:"onCreate,omitempty"`
	OnDeploy  zarf.ZarfComponentActionSet `json:"onDeploy,omitempty"`
	OnUpgrade zarf.ZarfComponentActionSet `json:"onUpgrade,omitempty"`
	OnRemove  zarf.ZarfComponentActionSet `json:"onRemove,omitempty"`
}

type ZarfDistroPackageConfig struct {
	Binaries []ZarfBinaries        `json:"binaries,omitempty"`
	Config   ZarfDistroImageConfig `json:"imageConfig,omitempty"`
}

type ZarfDistroImageConfig struct {
	Compression string   `json:"compression,omitempty" jsonschema:"enum=none,enum=gz,enum=zstd"`
	Path        string   `json:"path,omitempty"`
	Images      []string `json:"images,omitempty"`
}

type ZarfBinaries struct {
	Source      string             `json:"source"`
	Shasum      string             `json:"shasum,omitempty"`
	Target      string             `json:"target"`
	Executable  bool               `json:"executable,omitempty"`
	Symlinks    []string           `json:"symlinks,omitempty"`
	ExtractPath string             `json:"extractPath,omitempty"`
	Selector    ZarfBinarySelector `json:"selector,omitempty"`
}

type ZarfBinarySelector struct {
	Roles    []string `json:"roles,omitempty"`
	Profiles []string `json:"profiles,omitempty"`
}
