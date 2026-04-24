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

type ZarfDistro struct {
	APIVersion string                  `json:"apiVersion,omitempty" jsonschema:"enum=zarf.dev/v1alpha1"`
	Kind       v1alpha1.ZarfDistroKind `json:"kind" jsonschema:"enum=ZarfDistro"`
	Metadata   ZarfDistroMetadata      `json:"metadata"`
	Build      ZarfDistroBuildData     `json:"build,omitempty"`
	Spec       ZarfDistroSpec          `json:"spec"`
}

type ZarfDistroMetadata struct {
	Uncompressed bool              `json:"uncompressed,omitempty" jsonschema:"default=false"`
	Architecture string            `json:"architecture,omitempty" jsonschema:"default=amd64,enum=amd64,enum=arm64"`
	Name         string            `json:"name" jsonschema:"pattern=^[a-z0-9][a-z0-9\\-]*$"`
	Description  string            `json:"description,omitempty"`
	Version      string            `json:"version,omitempty"`
	Annotations  map[string]string `json:"annotations,omitempty"`
}

type ZarfDistroBuildData struct {
	Architecture      string            `json:"architecture,omitempty"`
	Timestamp         string            `json:"timestamp,omitempty"`
	Version           string            `json:"version,omitempty"`
	RegistryOverrides map[string]string `json:"registryOverrides,omitempty"`
}

type ZarfDistroSpec struct {
	Type    string            `json:"type" jsonschema:"enum=rke2,enum=k3s,enum=upstream"`
	Version string            `json:"version"`
	Actions ZarfDistroActions `json:"actions,omitempty"`
	Config  ZarfDistroConfig  `json:"config"`
}

type ZarfDistroActions struct {
	OnCreate zarf.ZarfComponentActionSet `json:"onCreate,omitempty"`
	OnDeploy zarf.ZarfComponentActionSet `json:"onDeploy,omitempty"`
	OnRemove zarf.ZarfComponentActionSet `json:"onRemove,omitempty"`
}

type ZarfDistroConfig struct {
	// Files are files that will be populated on the hosts, regardless of what install method is used
	Files        v1alpha1.ZarfFiles    `json:"files,omitempty"`
	ImagesConfig ZarfDistroImageConfig `json:"imageConfig,omitempty"`
	OS           ZarfDistroOS          `json:"os,omitempty"`
	Engine       dig.Mapping           `json:"engine,omitempty"`
}

type ZarfDistroImageConfig struct {
	Compression string   `json:"compression,omitempty" jsonschema:"default=none,enum=none,enum=gz,enum=zstd"`
	Path        string   `json:"path,omitempty"`
	Images      []string `json:"images,omitempty"`
}

type ZarfDistroOS struct {
	Sysctl    map[string]string  `json:"sysctl,omitempty"`
	FAPolicyd string             `json:"fapolicyd,omitempty"`
	Files     v1alpha1.ZarfFiles `json:"files,omitempty"`
}

func (distro ZarfDistro) IsSBOMAble() bool {
	if len(distro.Spec.Config.ImagesConfig.Images) > 0 || len(distro.Spec.Config.Files) > 0 {
		return true
	}
	return false
}
