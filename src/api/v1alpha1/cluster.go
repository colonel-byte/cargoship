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

import "github.com/k0sproject/rig"

type ZarfCluster struct {
	APIVersion string             `json:"apiVersion,omitempty" jsonschema:"enum=zarf.dev/v1alpha1"`
	Kind       ZarfDistroKind     `json:"kind" jsonschema:"enum=ZarfCluster"`
	Metadata   ZarfDistroMetadata `json:"metadata"`
	Spec       ZarfClusterSpec    `json:"spec"`
}

type ZarfClusterSpec struct {
	Config ZarfClusterConfig  `json:"config"`
	Hosts  []ZarfClusterHosts `json:"hosts" jsonschema:"minItems=1"`
}

type ZarfClusterConfig struct {
	Type       string                  `json:"type" jsonschema:"enum=rke2,enum=k3s"`
	Registries []ZarfClusterRegistries `json:"registries,omitempty"`
	Profiles   []ZarfClusterProfiles   `json:"profiles,omitempty"`
}

type ZarfClusterProfiles struct {
	Name    string         `json:"name"`
	Kubelet map[string]any `json:"kubeletConfig,omitempty"`
	Engine  map[string]any `json:"engineConfig,omitempty"`
}

type ZarfClusterRegistries struct {
	Name           string                   `json:"name"`
	Authentication ZarfClusterRegistryAuth  `json:"auth,omitempty"`
	Proxy          ZarfClusterRegistryProxy `json:"proxy"`
}

type ZarfClusterRegistryAuth struct {
	Username string `json:"user,omitempty"`
	Password string `json:"pass,omitempty"`
	Token    string `json:"token,omitempty"`
}

type ZarfClusterRegistryProxy struct {
	URL string `json:"url"`
}

type ZarfClusterHosts struct {
	Role             string             `json:"role" jsonschema:"enum=controller,enum=worker"`
	Profile          string             `json:"profile,omitempty" `
	Hostname         string             `json:"hostname"`
	Environment      map[string]string  `json:"environment,omitempty"`
	Files            []ZarfClusterFiles `json:"files,omitempty"`
	PrivateInterface string             `json:"privateInterface,omitempty"`
	PrivateAddress   string             `json:"privateAddress,omitempty"`
	DataDirectory    string             `json:"dataDir,omitempty"`
	KubeletDirectory string             `json:"kubeletRootDir,omitempty"`
	Connection       rig.OpenSSH        `json:"ssh" jsonschema:""`
}

type ZarfClusterFiles struct {
	Name                 string `json:"name"`
	Source               string `json:"src,omitempty"`
	Destination          string `json:"dst,omitempty"`
	DestinationDirectory string `json:"dstDir,omitempty"`
	Permission           string `json:"perm,omitempty"`
	User                 string `json:"user,omitempty" jsonschema:"example=root"`
	Group                string `json:"group,omitempty" jsonschema:"example=root"`
	Data                 string `json:"data,omitempty"`
}
