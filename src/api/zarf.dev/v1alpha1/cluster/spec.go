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

// Package cluster is for the api representation of Cluster
package cluster

import (
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1"
)

// ZarfCluster root for a cluster config
type ZarfCluster struct {
	APIVersion      string                  `json:"apiVersion,omitempty" jsonschema:"enum=zarf.dev/v1alpha1"`
	Kind            v1alpha1.ZarfDistroKind `json:"kind" jsonschema:"enum=ZarfCluster"`
	Metadata        ZarfClusterMetadata     `json:"metadata"`
	Spec            ZarfClusterSpec         `json:"spec"`
	RuntimeMetadata ZarfRuntimeMeta         `json:"-"`
}

// ZarfClusterMetadata a cluster config
type ZarfClusterMetadata struct {
	Name        string            `json:"name" jsonschema:"pattern=^[a-z0-9][a-z0-9\\-]*$"`
	Description string            `json:"description,omitempty"`
	Version     string            `json:"version,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ZarfRuntimeMeta for storing data when running the various phases
type ZarfRuntimeMeta struct {
	ControllerTLS   []string
	ControllerToken string
	AgentToken      string
	LoadBalancer    string
	Leader          *ZarfHost
}

// ZarfClusterSpec a cluster config
type ZarfClusterSpec struct {
	Config ZarfClusterConfig `json:"config"`
	Hosts  ZarfHosts         `json:"hosts" jsonschema:"minItems=1"`
}

// ZarfClusterConfig for a cluster
type ZarfClusterConfig struct {
	LoadBalancer string                  `json:"loadbalancer" jsonschema:"format=hostname"`
	Registries   []ZarfClusterRegistries `json:"registries,omitempty"`
	Profiles     []ZarfClusterProfiles   `json:"profiles,omitempty"`
}

// ZarfClusterProfiles for the engine
type ZarfClusterProfiles struct {
	Name    string         `json:"name"`
	Kubelet map[string]any `json:"kubeletConfig,omitempty"`
	Engine  map[string]any `json:"engineConfig,omitempty"`
}

// ZarfClusterRegistries overrides
type ZarfClusterRegistries struct {
	// Name of the registry
	Name string `json:"name"`
	// Authentication for the registry
	Authentication ZarfClusterRegistryAuth `json:"auth,omitempty"`
	// Proxy for the registry
	Proxy ZarfClusterRegistryProxy `json:"proxy"`
}

// ZarfClusterRegistryAuth information
type ZarfClusterRegistryAuth struct {
	// Username for the remote registry
	Username string `json:"user,omitempty"`
	// Password for the remote registry
	Password string `json:"pass,omitempty"`
	// Token for the remote registry
	Token string `json:"token,omitempty"`
}

// ZarfClusterRegistryProxy override for the registry information
type ZarfClusterRegistryProxy struct {
	// URL to the registry that will engine will now pull from
	URL string `json:"url"`
}

// ZarfClusterFiles data
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
