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
	"fmt"

	"github.com/colonel-byte/cargoship/api"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1"
	"github.com/invopop/jsonschema"
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
	Build ZarfDistroBuildData `json:"build,omitempty"`
	// Spec holds the configuration for the distro package.
	Spec ZarfDistroSpec `json:"spec"`
}

// ZarfDistroMetadata holds identifying information for a distro package.
type ZarfDistroMetadata struct {
	// Uncompressed disables compression for this package when true.
	Uncompressed bool `json:"uncompressed,omitempty"`
	// Architecture is the CPU architecture this distro package targets. Use Architectures to target more than one.
	Architecture api.Arch `json:"architecture,omitempty" jsonschema:"default=amd64"`
	// Architectures lists the CPU architectures this distro package targets. It supersedes Architecture, which stays valid for a package targeting a single architecture.
	Architectures api.Arches `json:"architectures,omitempty"`
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

// FileSource records where one file in the package was actually read from, for a file whose
// declared source a --file-override redirected.
//
// This is evidence about a build that happened, which is what separates it from
// ZarfDistroBuildData.FileOverrides: an override can be configured and match nothing, and the
// configuration alone cannot tell you which of several overlapping prefixes won for a given
// file. Resolved names a path on the build host when the override pointed at a local
// directory, so it discloses that much of the build host's layout to whoever reads the package.
type FileSource struct {
	// Path is where the file was staged inside the package, for example "files/0/k3s". It
	// identifies the entry even when several files share a declared source.
	Path string `json:"path"`
	// Declared is the source the distro definition named.
	Declared string `json:"declared"`
	// Resolved is where cargoship read the bytes from instead: a mirror URL, or a path on
	// the build host.
	Resolved string `json:"resolved"`
	// Override is the Source prefix of the override that matched, which is the one that won
	// when several could have.
	Override string `json:"override"`
	// Shasum is the checksum the bytes were verified against. An override is refused for a
	// file that declares none, so this is never empty.
	Shasum string `json:"shasum"`
}

// ZarfDistroBuildData holds information recorded when the package was built.
type ZarfDistroBuildData struct {
	// Architecture is the CPU architecture used to build the package. Populated only when the package targets a single architecture.
	Architecture api.Arch `json:"architecture,omitempty"`
	// Architectures lists the CPU architectures used to build the package.
	Architectures api.Arches `json:"architectures,omitempty"`
	// Timestamp is the time the package was created.
	Timestamp string `json:"timestamp,omitempty"`
	// Version records the distro version used to build the package.
	Version string `json:"version,omitempty"`
	// RegistryOverrides maps each original registry to the registry actually used to build the package.
	RegistryOverrides map[string]string `json:"registryOverrides,omitempty"`
	// FileOverrides maps each source URL prefix to the mirror or local directory files were
	// downloaded from instead. Like RegistryOverrides, this is the configuration the build was
	// given. For what each override actually did, see FileSources.
	FileOverrides map[string]string `json:"fileOverrides,omitempty"`
	// FileSources records every file whose source an override redirected: what the definition
	// declared, where the bytes were read from instead, and the checksum they were verified
	// against. It is empty for a build that used no overrides, so a package built without them
	// is unchanged. Entries are in the order the files were staged, which is the order they
	// appear in the definition, so a reproducible build still produces identical output.
	FileSources []FileSource `json:"fileSources,omitempty"`
	// Signed indicates whether the package was signed. A nil value means the signing status was not recorded.
	Signed *bool `json:"signed,omitempty"`
	// Reproducible indicates Build.Timestamp was pinned to a fixed value
	// (config.Timestamp) instead of the actual build time, so identical
	// package inputs produce byte-identical output.
	Reproducible bool `json:"reproducible,omitempty"`
	// ProvenanceFiles lists files in the package that checksums.txt does not cover.
	// These are files added after cargoship generates checksums, for example signature files.
	// The signed distro.yaml authenticates this list.
	ProvenanceFiles []string `json:"provenanceFiles,omitempty"`
}

// ZarfDistroSpec holds the configuration for a distro package.
type ZarfDistroSpec struct {
	// Type selects the distro engine: rke2, k3s, or upstream.
	Type string `json:"type" jsonschema:"enum=rke2,enum=k3s,enum=upstream"`
	// Version is the version of the distro engine.
	Version string `json:"version"`
	// Actions defines the actions cargoship runs while building the package.
	Actions ZarfDistroActions `json:"actions,omitempty"`
	// Config holds the distro engine configuration.
	Config ZarfDistroConfig `json:"config"`
	// Values holds the default values the package is built with, and the schema they must satisfy.
	Values ZarfDistroValues `json:"values,omitempty"`
}

// ZarfDistroValues declares the values a package ships with. Values are the
// Helm-style configuration described by ZEP-0021: a nested structure addressed
// by dotted paths, which templates in the package read as .Values.
type ZarfDistroValues struct {
	// Files lists the YAML values files cargoship merges, in order, to build the package's default values. Each entry is a path relative to the package definition, an absolute path, or a URL. A later file overrides an earlier one, key by key.
	Files []string `json:"files,omitempty" jsonschema:"example=values.yaml,example=overrides/prod.yaml,example=https://example.com/values.yaml"`
	// Schema is the JSON Schema document the merged values must satisfy. Cargoship checks the values against it when it builds the package, and refuses to build when they do not match. The schema must not use $ref.
	Schema string `json:"schema,omitempty" jsonschema:"example=values.schema.json"`
	// Mappings project values onto the engine configuration, so that one value a
	// cluster sets reaches wherever the distro needs it. Each mapping reads the
	// source path out of the resolved values and writes it to the target path,
	// which is relative to spec.config.engine. A source the values do not define
	// is left alone, so the package's own engine configuration stands as the
	// default.
	Mappings []ZarfDistroValueMapping `json:"mappings,omitempty"`
}

// ZarfDistroValueMapping projects one value onto one place in the engine configuration.
type ZarfDistroValueMapping struct {
	// Source is the dotted path to read from the resolved values, e.g. .cilium.encryption.enabled
	Source string `json:"source" jsonschema:"example=.cilium.encryption.enabled"`
	// Target is the dotted path to write, relative to spec.config.engine,
	// e.g. .manifest.rke2-cilium.encryption.enabled
	Target string `json:"target" jsonschema:"example=.manifest.rke2-cilium.encryption.enabled"`
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
	// Manifests lists on-host paths -- each one the Target of a Files entry -- that cargoship
	// kubectl-applies, in order, from the leader once it bootstraps. Delivery is already solved by
	// Files; this is only the marker that says "and apply this one," e.g. for a CNI a package
	// declares. A distro with no apply mechanism (rke2/k3s use HelmChartConfig instead) ignores it.
	Manifests []string `json:"manifests,omitempty"`
}

// JSONSchemaExtend pins down the shape of the engine's manifest section, whose values are Helm
// values written into a HelmChartConfig: either a YAML string or a mapping cargoship serializes
// to YAML for the chart. Engine is otherwise a free-form mapping handed to the distro engine, so
// the section list stays open and every other section keeps validating as it did.
func (ZarfDistroConfig) JSONSchemaExtend(s *jsonschema.Schema) {
	engine, ok := s.Properties.Get("engine")
	if !ok {
		return
	}
	manifest := &jsonschema.Schema{
		Type:        "object",
		Description: "maps a chart name to the Helm values cargoship writes to that chart's HelmChartConfig. A value is either a YAML string or a mapping.",
		AdditionalProperties: &jsonschema.Schema{
			OneOf: []*jsonschema.Schema{
				{Type: "string"},
				{Type: "object"},
			},
		},
	}

	// Replacing the $ref with an inline object is what lets a property be described at all:
	// the Mapping definition is shared by every free-form mapping in the schema.
	engine.Ref = ""
	engine.Type = "object"
	engine.Properties = jsonschema.NewProperties()
	engine.Properties.Set("manifest", manifest)
}

// Compression formats accepted by ZarfDistroImageConfig.Compression.
const (
	// CompressionNone writes the image tarballs uncompressed. This is the default.
	CompressionNone = "none"
	// CompressionGzip writes the image tarballs with gzip compression.
	CompressionGzip = "gz"
	// CompressionZstd writes the image tarballs with zstd compression.
	CompressionZstd = "zstd"
)

// ZarfDistroImageConfig holds settings for the images cargoship writes to a host.
type ZarfDistroImageConfig struct {
	// Compression sets the compression format for the image tarballs.
	Compression string `json:"compression,omitempty" jsonschema:"default=none,enum=none,enum=gz,enum=zstd"`
	// Path is the upload destination for the image tarballs.
	Path string `json:"path,omitempty"`
	// Images lists the offline images required by the package.
	Images []string `json:"images,omitempty" jsonschema:"uniqueItems=true"`
}

// TarballSuffix returns the file suffix image tarballs get for the configured
// compression format. The suffixes match the archive extensions a host imports,
// so a compressed tarball is still picked up. An unset format means no
// compression. It returns an error for a format cargoship cannot write.
func (c ZarfDistroImageConfig) TarballSuffix() (string, error) {
	switch c.Compression {
	case "", CompressionNone:
		return ".tar", nil
	case CompressionGzip:
		return ".tar.gz", nil
	case CompressionZstd:
		return ".tar.zst", nil
	default:
		return "", fmt.Errorf("unsupported image compression %q, expected one of %q, %q, %q", c.Compression, CompressionNone, CompressionGzip, CompressionZstd)
	}
}

// ZarfDistroOS holds settings applied to a host.
type ZarfDistroOS struct {
	// Sysctl maps sysctl keys to the values cargoship applies to a host.
	Sysctl map[string]string `json:"sysctl,omitempty"`
	// FAPolicyd holds the fapolicyd config file contents cargoship writes to a host.
	FAPolicyd string `json:"fapolicyd,omitempty"`
	// SELinux holds the custom SELinux policy cargoship applies to a host.
	SELinux ZarfDistroSELinux `json:"selinux,omitempty"`
	// Files lists files cargoship uploads to a host.
	Files v1alpha1.ZarfFiles `json:"files,omitempty"`
	// Kernel lists the kernel modules cargoship enables on the host.
	Kernel []string `json:"kernel,omitempty"`
	// Environment maps environment variables cargoship sets on the host.
	Environment map[string]string `json:"env,omitempty"`
}

// JSONSchemaExtend widens sysctl values to accept numbers alongside strings, so
// unquoted numeric values in YAML validate.
func (ZarfDistroOS) JSONSchemaExtend(s *jsonschema.Schema) {
	sysctl, ok := s.Properties.Get("sysctl")
	if !ok {
		return
	}
	sysctl.AdditionalProperties = &jsonschema.Schema{
		OneOf: []*jsonschema.Schema{
			{Type: "string"},
			{Type: "number"},
		},
	}
}

// ZarfDistroSELinux holds the custom SELinux policy cargoship applies to hosts running SELinux
// in enforcing mode. Every field is optional, and a host that is not enforcing is left alone.
type ZarfDistroSELinux struct {
	// Modules lists the CIL policy modules cargoship installs on a host.
	Modules []ZarfDistroSELinuxModule `json:"modules,omitempty"`
	// Booleans maps SELinux boolean names to the values cargoship sets persistently on a host.
	Booleans map[string]bool `json:"booleans,omitempty"`
	// FileContexts lists the file context mappings cargoship adds on a host.
	FileContexts []ZarfDistroSELinuxFileContext `json:"fileContexts,omitempty"`
}

// IsZero reports whether no SELinux policy is configured, so a phase can gate on it.
func (s ZarfDistroSELinux) IsZero() bool {
	return len(s.Modules) == 0 && len(s.Booleans) == 0 && len(s.FileContexts) == 0
}

// ZarfDistroSELinuxModule holds a single CIL policy module cargoship installs with semodule.
type ZarfDistroSELinuxModule struct {
	// Name identifies the module to semodule, and names the file cargoship writes to the host.
	Name string `json:"name" jsonschema:"pattern=^[a-zA-Z0-9][a-zA-Z0-9_-]*$"`
	// Priority holds the semodule priority the module installs at.
	Priority int `json:"priority,omitempty" jsonschema:"default=400"`
	// CIL holds the Common Intermediate Language policy source semodule installs.
	CIL string `json:"cil"`
}

// ZarfDistroSELinuxFileContext holds a single file context mapping cargoship adds with semanage.
type ZarfDistroSELinuxFileContext struct {
	// Path holds the path regular expression the mapping applies to.
	Path string `json:"path" jsonschema:"example=/var/lib/rancher(/.*)?"`
	// Type names the SELinux type the matching paths are labeled with.
	Type string `json:"type" jsonschema:"example=container_var_lib_t"`
	// FileType restricts the mapping to one kind of filesystem object.
	FileType string `json:"fileType,omitempty" jsonschema:"default=all,enum=all,enum=file,enum=dir,enum=symlink,enum=pipe,enum=socket,enum=block,enum=char"`
}

// Arches returns the CPU architectures the package targets. It prefers Architectures and falls
// back to the single Architecture field, so callers never have to know which one the package set.
func (m ZarfDistroMetadata) Arches() api.Arches {
	if len(m.Architectures) > 0 {
		return m.Architectures
	}
	if m.Architecture == "" {
		return nil
	}
	return api.Arches{m.Architecture}
}

// Arches returns the CPU architectures the package was built for. It prefers Architectures and
// falls back to the single Architecture field.
func (b ZarfDistroBuildData) Arches() api.Arches {
	if len(b.Architectures) > 0 {
		return b.Architectures
	}
	if b.Architecture == "" {
		return nil
	}
	return api.Arches{b.Architecture}
}

// Arches returns the CPU architectures the package covers. A built package records them under
// build, so that is preferred; a definition that has not been built yet only carries what the
// metadata targets.
func (distro ZarfDistro) Arches() api.Arches {
	if arches := distro.Build.Arches(); len(arches) > 0 {
		return arches
	}
	return distro.Metadata.Arches()
}

// IsSBOMAble reports whether cargoship can generate an SBOM for this distro package. It returns true if the config lists any images or files.
func (distro ZarfDistro) IsSBOMAble() bool {
	if len(distro.Spec.Config.ImagesConfig.Images) > 0 || len(distro.Spec.Config.Files) > 0 {
		return true
	}
	return false
}
