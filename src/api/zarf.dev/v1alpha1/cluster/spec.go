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

package cluster

import (
	"github.com/colonel-byte/mare/src/api/zarf.dev/v1alpha1"
)

type ZarfCluster struct {
	APIVersion      string                  `json:"apiVersion,omitempty" jsonschema:"enum=zarf.dev/v1alpha1"`
	Kind            v1alpha1.ZarfDistroKind `json:"kind" jsonschema:"enum=ZarfCluster"`
	Metadata        ZarfClusterMetadata     `json:"metadata"`
	Spec            ZarfClusterSpec         `json:"spec"`
	RuntimeMetadata ZarfRuntimeMeta         `json:"-"`
}

type ZarfClusterMetadata struct {
	Name        string            `json:"name" jsonschema:"pattern=^[a-z0-9][a-z0-9\\-]*$"`
	Description string            `json:"description,omitempty"`
	Version     string            `json:"version,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type ZarfRuntimeMeta struct {
	ControllerTLS   []string
	ControllerToken string
	AgentToken      string
	LoadBalancer    string
	Leader          *ZarfHost
}

type ZarfClusterSpec struct {
	Config ZarfClusterConfig `json:"config"`
	Hosts  ZarfHosts         `json:"hosts" jsonschema:"minItems=1"`
}

type ZarfClusterConfig struct {
	LoadBalancer string                  `json:"loadbalancer" jsonschema:"format=hostname"`
	Registries   []ZarfClusterRegistries `json:"registries,omitempty"`
	Profiles     []ZarfClusterProfiles   `json:"profiles,omitempty"`
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
