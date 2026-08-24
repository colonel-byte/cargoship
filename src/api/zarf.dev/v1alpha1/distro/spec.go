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

// Package distro defines the API types for a distro package.
package distro

import (
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1"
	"github.com/k0sproject/dig"
	zarf "github.com/zarf-dev/zarf/src/api/v1alpha1"
)

// ZarfDistro is the root object of a distro package configuration document.
type ZarfDistro struct {
	// APIVersion identifies the API group and version of this configuration document.
	APIVersion string `json:"apiVersion,omitempty" jsonschema:"enum=zarf.dev/v1alpha1"`
	// Kind identifies the document type. The value must be ZarfDistro.
	Kind v1alpha1.ZarfDistroKind `json:"kind" jsonschema:"enum=ZarfDistro"`
	// Metadata holds identifying information for the distro package.
	Metadata ZarfDistroMetadata `json:"metadata"`
	// Build holds information recorded when the package was built.
	Build ZarfDistroBuildData `json:"-"`
	// Spec holds the configuration for the distro package.
	Spec ZarfDistroSpec `json:"spec"`
}

// ZarfDistroMetadata holds identifying information for a distro package.
type ZarfDistroMetadata struct {
	// Uncompressed disables compression for this package when true.
	Uncompressed bool `json:"uncompressed,omitempty"`
	// Architecture is the CPU architecture this distro package targets.
	Architecture string `json:"architecture,omitempty" jsonschema:"default=amd64,enum=amd64,enum=arm64"`
	// Name identifies the distro package.
	Name string `json:"name" jsonschema:"pattern=^[a-z0-9][a-z0-9\\-]*$"`
	// Description explains what this distro package does.
	Description string `json:"description,omitempty"`
	// Version is the distro version cargoship installs. We recommend matching it to the Kubernetes version you install.
	Version string `json:"version,omitempty"`
	// Annotations holds key-value pairs added to the OCI manifest.
	Annotations map[string]string `json:"annotations,omitempty"`
	// URL sets the OCI annotation for more information about the image.
	URL string `json:"url,omitempty"`
	// Authors sets the OCI annotation for the contact details of the people or organization responsible for the image.
	Authors string `json:"athors,omitempty"`
	// Documentation sets the OCI annotation for the URL to the image documentation.
	Documentation string `json:"documentation,omitempty"`
	// Source sets the OCI annotation for the URL to the image source code.
	Source string `json:"source,omitempty"`
	// Vendor sets the OCI annotation for the name of the organization or individual that distributes the image.
	Vendor string `json:"vendor,omitempty"`
	// AggregateChecksum is the checksum of the checksums.txt file, which lists the checksum for every layer in the package.
	AggregateChecksum string `json:"aggregateChecksum,omitempty"`
}

// ZarfDistroBuildData holds information recorded when the package was built.
type ZarfDistroBuildData struct {
	// Architecture is the CPU architecture used to build the package.
	Architecture string `json:"architecture,omitempty"`
	// Timestamp is the time the package was created.
	Timestamp string `json:"timestamp,omitempty"`
	// Version records the distro version used to build the package.
	Version string `json:"version,omitempty"`
	// RegistryOverrides maps each original registry to the registry actually used to build the package.
	RegistryOverrides map[string]string `json:"registryOverrides,omitempty"`
	// Signed indicates whether the package was signed. A nil value means the signing status was not recorded.
	Signed *bool `json:"signed,omitempty"`
	// Reproducible indicates Build.Timestamp was pinned to the Unix epoch
	// (2026-03-27T22:40:34Z) instead of the actual build time, so identical
	// package inputs produce byte-identical output.
	Reproducible bool `json:"reproducible,omitempty"`
	// ProvenanceFiles lists files in the package that checksums.txt does not cover.
	// These are files added after cargoship generates checksums, for example signature files.
	// The signed distro.yaml authenticates this list.
	ProvenanceFiles []string `json:"provenanceFiles,omitempty"`
}

// ZarfDistroSpec holds the configuration for a distro package.
type ZarfDistroSpec struct {
	// Type selects the distro engine: rke2 or k3s.
	Type string `json:"type" jsonschema:"enum=rke2,enum=k3s"`
	// Version is the version of the distro engine.
	Version string `json:"version"`
	// Actions defines the actions cargoship runs while building the package.
	Actions ZarfDistroActions `json:"actions,omitempty"`
	// Config holds the distro engine configuration.
	Config ZarfDistroConfig `json:"config"`
}

// ZarfDistroActions defines the actions cargoship runs during specific phases of building the distro package.
type ZarfDistroActions struct {
	// OnCreate lists the actions cargoship runs when it creates the package.
	OnCreate zarf.ZarfComponentActionSet `json:"onCreate,omitempty"`
}

// ZarfDistroConfig holds the configuration for the distro engine.
type ZarfDistroConfig struct {
	// Files lists files that cargoship writes to every host, no matter which install method it uses.
	Files v1alpha1.ZarfFiles `json:"files,omitempty"`
	// ImagesConfig holds settings for the images bundled with the package.
	ImagesConfig ZarfDistroImageConfig `json:"imageConfig,omitempty"`
	// OS holds settings applied to the host operating system.
	OS ZarfDistroOS `json:"os,omitempty"`
	// Engine holds configuration passed through to the distro engine.
	Engine dig.Mapping `json:"engine,omitempty"`
}

// ZarfDistroImageConfig holds settings for the images cargoship writes to a host.
type ZarfDistroImageConfig struct {
	// Compression sets the compression format for the image tarballs.
	Compression string `json:"compression,omitempty" jsonschema:"default=none,enum=none,enum=gz,enum=zstd"`
	// Path is the upload destination for the image tarballs.
	Path string `json:"path,omitempty"`
	// Images lists the offline images required by the package.
	Images []string `json:"images,omitempty" jsonschema:"uniqueItems=true"`
}

// ZarfDistroOS holds settings applied to a host.
type ZarfDistroOS struct {
	// Sysctl maps sysctl keys to the values cargoship applies to a host.
	Sysctl map[string]string `json:"sysctl,omitempty"`
	// FAPolicyd holds the fapolicyd config file contents cargoship writes to a host.
	FAPolicyd string `json:"fapolicyd,omitempty"`
	// Files lists files cargoship uploads to a host.
	Files v1alpha1.ZarfFiles `json:"files,omitempty"`
	// Kernel lists the kernel modules cargoship enables on the host.
	Kernel []string `json:"kernel,omitempty"`
	// Environment maps environment variables cargoship sets on the host.
	Environment map[string]string `json:"env,omitempty"`
}

// IsSBOMAble reports whether cargoship can generate an SBOM for this distro package. It returns true if the config lists any images or files.
func (distro ZarfDistro) IsSBOMAble() bool {
	if len(distro.Spec.Config.ImagesConfig.Images) > 0 || len(distro.Spec.Config.Files) > 0 {
		return true
	}
	return false
}
