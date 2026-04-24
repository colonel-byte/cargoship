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

// Package distro is for the api representation of Distro Package
package distro

import (
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1"
	"github.com/k0sproject/dig"
	zarf "github.com/zarf-dev/zarf/src/api/v1alpha1"
)

// ZarfDistro root information
type ZarfDistro struct {
	APIVersion string                  `json:"apiVersion,omitempty" jsonschema:"enum=zarf.dev/v1alpha1"`
	Kind       v1alpha1.ZarfDistroKind `json:"kind" jsonschema:"enum=ZarfDistro"`
	Metadata   ZarfDistroMetadata      `json:"metadata"`
	Build      ZarfDistroBuildData     `json:"build,omitempty"`
	Spec       ZarfDistroSpec          `json:"spec"`
}

// ZarfDistroMetadata for the distro package
type ZarfDistroMetadata struct {
	Uncompressed bool              `json:"uncompressed,omitempty" jsonschema:"default=false"`
	Architecture string            `json:"architecture,omitempty" jsonschema:"default=amd64,enum=amd64,enum=arm64"`
	Name         string            `json:"name" jsonschema:"pattern=^[a-z0-9][a-z0-9\\-]*$"`
	Description  string            `json:"description,omitempty"`
	Version      string            `json:"version,omitempty"`
	Annotations  map[string]string `json:"annotations,omitempty"`
}

// ZarfDistroBuildData time information
type ZarfDistroBuildData struct {
	// Architecture of the distro package
	Architecture string `json:"architecture,omitempty"`
	// Timestamp of when the distro was created
	Timestamp string `json:"timestamp,omitempty"`
	// Version of the distro is created as
	Version string `json:"version,omitempty"`
	// RegistryOverrides for who the distro was created with
	RegistryOverrides map[string]string `json:"registryOverrides,omitempty"`
}

// ZarfDistroSpec that manage the distro spec
type ZarfDistroSpec struct {
	// Type of distro that is being created
	Type string `json:"type" jsonschema:"enum=rke2,enum=k3s"`
	// Version of the engine
	Version string `json:"version"`
	// Actions that are ran during some package phases
	Actions ZarfDistroActions `json:"actions,omitempty"`
	// Config for the distro
	Config ZarfDistroConfig `json:"config"`
}

// ZarfDistroActions that are ran during certain phases of the distro package
type ZarfDistroActions struct {
	// OnCreate actions
	OnCreate zarf.ZarfComponentActionSet `json:"onCreate,omitempty"`
}

// ZarfDistroConfig holds values for distro config
type ZarfDistroConfig struct {
	// Files are files that will be populated on the hosts, regardless of what install method is used
	Files v1alpha1.ZarfFiles `json:"files,omitempty"`
	// ImagesConfig for the images
	ImagesConfig ZarfDistroImageConfig `json:"imageConfig,omitempty"`
	// OS for the node os
	OS ZarfDistroOS `json:"os,omitempty"`
	// Engine is used for configuring the engine
	Engine dig.Mapping `json:"engine,omitempty"`
}

// ZarfDistroImageConfig holds values for the images that will be populated on a host
type ZarfDistroImageConfig struct {
	// Compression that the image tar balls will be compressed with
	Compression string `json:"compression,omitempty" jsonschema:"default=none,enum=none,enum=gz,enum=zstd"`
	// Path that the image tar balls will be uploaded too
	Path string `json:"path,omitempty"`
	// Images list of the various required offline images
	Images []string `json:"images,omitempty"`
}

// ZarfDistroOS holds specific values for apply to a host
type ZarfDistroOS struct {
	// Sysctl a map of sysctl values that will be applied to a host
	Sysctl map[string]string `json:"sysctl,omitempty"`
	// FAPolicyd config file contents that will be applied to a host
	FAPolicyd string `json:"fapolicyd,omitempty"`
	// Files that will be uploaded to a host
	Files v1alpha1.ZarfFiles `json:"files,omitempty"`
}

// IsSBOMAble has files that can have a sbom generated from
func (distro ZarfDistro) IsSBOMAble() bool {
	if len(distro.Spec.Config.ImagesConfig.Images) > 0 || len(distro.Spec.Config.Files) > 0 {
		return true
	}
	return false
}
