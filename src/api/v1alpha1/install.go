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

type ZarfDistroInstall struct {
	APIVersion string                `json:"apiVersion,omitempty" jsonschema:"enum=zarf.dev/v1alpha1"`
	Kind       ZarfDistroKind        `json:"kind" jsonschema:"enum=ZarfDistroInstall"`
	Metadata   ZarfDistroMetadata    `json:"metadata"`
	Spec       ZarfDistroInstallSpec `json:"spec"`
}

type ZarfDistroInstallSpec struct {
	Config ZarfDistroInstallConfig  `json:"config"`
	Hosts  []ZarfDistroInstallHosts `json:"hosts" jsonschema:"minItems=1"`
}

type ZarfDistroInstallConfig struct {
	Type       string                        `json:"type" jsonschema:"enum=rke2,enum=k3s"`
	Registries []ZarfDistroInstallRegistries `json:"registries,omitempty"`
	Profiles   []ZarfDistroInstallProfiles   `json:"profiles,omitempty"`
}

type ZarfDistroInstallProfiles struct {
	Name    string         `json:"name"`
	Kubelet map[string]any `json:"kubeletConfig,omitempty"`
	Engine  map[string]any `json:"engineConfig,omitempty"`
}

type ZarfDistroInstallRegistries struct {
	Name           string                         `json:"name"`
	Authentication ZarfDistroInstallRegistryAuth  `json:"auth,omitempty"`
	Proxy          ZarfDistroInstallRegistryProxy `json:"proxy"`
}

type ZarfDistroInstallRegistryAuth struct {
	Username string `json:"user,omitempty"`
	Password string `json:"pass,omitempty"`
	Token    string `json:"token,omitempty"`
}

type ZarfDistroInstallRegistryProxy struct {
	URL string `json:"url"`
}

type ZarfDistroInstallHosts struct {
	Role             string                   `json:"role" jsonschema:"enum=controller,enum=worker"`
	Profile          string                   `json:"profile,omitempty" `
	Hostname         string                   `json:"hostname"`
	Environment      map[string]string        `json:"environment,omitempty"`
	Files            []ZarfDistroInstallFiles `json:"files,omitempty"`
	PrivateInterface string                   `json:"privateInterface,omitempty"`
	PrivateAddress   string                   `json:"privateAddress,omitempty"`
	DataDirectory    string                   `json:"dataDir,omitempty"`
	KubeletDirectory string                   `json:"kubeletRootDir,omitempty"`
	Connection       rig.OpenSSH              `json:"ssh" jsonschema:""`
}

type ZarfDistroInstallFiles struct {
	Name                 string `json:"name"`
	Source               string `json:"src,omitempty"`
	Destination          string `json:"dst,omitempty"`
	DestinationDirectory string `json:"dstDir,omitempty"`
	Permission           string `json:"perm,omitempty"`
	User                 string `json:"user,omitempty" jsonschema:"example=root"`
	Group                string `json:"group,omitempty" jsonschema:"example=root"`
	Data                 string `json:"data,omitempty"`
}
