// Copyright 2023, k0sctl authors.
// Copyright 2026 colonel-byte
//
// This file contains code derived from k0sctl:
// https://github.com/k0sproject/k0sctl
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

// Package cluster defines the API types for a cluster configuration.
package cluster

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1"
)

// ZarfCluster is the root object of a cluster configuration document.
type ZarfCluster struct {
	// APIVersion identifies the API group and version of this configuration document.
	APIVersion string `json:"apiVersion,omitempty" jsonschema:"enum=zarf.dev/v1alpha1"`
	// Kind identifies the document type. The value must be ZarfCluster.
	Kind v1alpha1.ZarfDistroKind `json:"kind" jsonschema:"enum=ZarfCluster"`
	// Metadata holds identifying information for the cluster.
	Metadata ZarfClusterMetadata `json:"metadata"`
	// Spec holds the configuration and hosts for the cluster.
	Spec ZarfClusterSpec `json:"spec"`
	// RuntimeMetadata stores data gathered while the phases run.
	RuntimeMetadata ZarfRuntimeMeta `json:"-"`
}

// ZarfClusterMetadata holds identifying information for a cluster.
type ZarfClusterMetadata struct {
	// Name sets the cluster name. If you allow cargoship to update the kubeconfig, cargoship uses this name there.
	Name string `json:"name" jsonschema:"pattern=^[a-z0-9][a-z0-9\\-]*$"`
}

// ZarfRuntimeMeta stores data gathered while the phases run.
type ZarfRuntimeMeta struct {
	// ControllerTLS lists the names and addresses on the controller TLS certificate.
	ControllerTLS []string
	// ControllerToken authorizes a worker node to join the cluster as a controller.
	ControllerToken string
	// AgentToken authorizes a worker node to join the cluster as an agent.
	AgentToken string
	// LoadBalancer is the hostname clients use to reach the cluster control plane.
	LoadBalancer string
	// Leader is the controller host that stores the cluster join tokens.
	Leader *ZarfHost
}

// ZarfClusterSpec holds the configuration and hosts for a cluster.
type ZarfClusterSpec struct {
	// Config holds the cluster-wide configuration.
	Config ZarfClusterConfig `json:"config"`
	// Hosts lists the hosts that make up the cluster.
	Hosts ZarfHosts `json:"hosts" jsonschema:"minItems=1"`
}

// ZarfClusterConfig holds cluster-wide configuration.
type ZarfClusterConfig struct {
	// LoadBalancer is the hostname clients use to reach the cluster control plane.
	LoadBalancer string `json:"loadbalancer" jsonschema:"format=hostname"`
	// Registries lists the container registries the cluster uses.
	Registries []ZarfClusterRegistries `json:"registries,omitempty"`
	// Profiles maps a profile name to host and engine overrides that a host can select.
	Profiles map[string]ZarfClusterProfiles `json:"profiles,omitempty"`
}

// ZarfClusterProfiles holds the host and engine overrides for one profile.
type ZarfClusterProfiles struct {
	// Host holds the configuration overrides applied to a host that selects this profile.
	Host ZarfHostConfig `json:"host,omitempty"`
	// Engine holds the node label and taint overrides applied to a host that selects this profile.
	Engine ZarfHostEngine `json:"engine,omitempty"`
	// Concurrency limits how many hosts using this profile cargoship processes at once, e.g.
	// draining, upgrading, initializing, or uninstalling. It accepts a fixed count ("1") or a
	// percentage of the hosts sharing this profile ("25%"). Empty falls back to the phase's
	// default concurrency.
	Concurrency string `json:"concurrency,omitempty" jsonschema:"oneof_type=string;integer" jsonschema_extras:"examples=1,examples=5,examples=25%,examples=100%"`
}

// ResolveConcurrency returns the batch size cargoship should use for total hosts sharing this
// profile. A fixed count is used as-is. A percentage (e.g. "25%") is scaled against total,
// rounded up, and clamped to a minimum of 1 so a low percentage never collapses to 0, which
// would otherwise be indistinguishable from "unlimited". An empty Concurrency falls back to
// fallback, parsed the same way against total.
func (p ZarfClusterProfiles) ResolveConcurrency(total int, fallback string) (int, error) {
	c := strings.TrimSpace(p.Concurrency)
	if c == "" {
		c = fallback
	}
	return ParseConcurrency(c, total)
}

// ParseConcurrency parses a concurrency spec (a fixed count like "1", or a percentage like
// "25%") against total, the number of hosts in the batch being sized. A fixed count is used
// as-is; 0 or empty means unlimited. A percentage is scaled against total, rounded up, and
// clamped to a minimum of 1 so a low percentage never collapses to 0, which would otherwise be
// indistinguishable from "unlimited".
func ParseConcurrency(value string, total int) (int, error) {
	c := strings.TrimSpace(value)
	if c == "" {
		return 0, nil
	}

	if pct, ok := strings.CutSuffix(c, "%"); ok {
		v, err := strconv.Atoi(strings.TrimSpace(pct))
		if err != nil {
			return 0, fmt.Errorf("invalid concurrency percentage %q: %w", value, err)
		}
		if v < 0 || v > 100 {
			return 0, fmt.Errorf("invalid concurrency percentage %q: must be between 0 and 100", value)
		}
		scaled := (total*v + 99) / 100
		if scaled < 1 {
			scaled = 1
		}
		return scaled, nil
	}

	v, err := strconv.Atoi(c)
	if err != nil {
		return 0, fmt.Errorf("invalid concurrency %q: %w", value, err)
	}
	if v < 0 {
		return 0, fmt.Errorf("invalid concurrency %q: must not be negative", value)
	}
	return v, nil
}

// ZarfClusterRegistries holds the credentials and pull proxy for one container registry.
type ZarfClusterRegistries struct {
	// Name identifies the registry.
	Name string `json:"name"`
	// Authentication holds the credentials for the registry.
	Authentication ZarfClusterRegistryAuth `json:"auth,omitempty"`
	// Proxy holds the pull redirect settings for the registry.
	Proxy ZarfClusterRegistryProxy `json:"proxy"`
}

// ZarfClusterRegistryAuth holds the credentials for a container registry.
type ZarfClusterRegistryAuth struct {
	// Username is the login name for the remote registry.
	Username string `json:"user,omitempty"`
	// Password is the login secret for the remote registry.
	Password string `json:"pass,omitempty"`
	// Token authenticates to the remote registry instead of a username and password.
	Token string `json:"token,omitempty"`
}

// ZarfClusterRegistryProxy redirects pulls for a registry to a different URL.
type ZarfClusterRegistryProxy struct {
	// URL is the registry address the engine pulls from instead of the original registry.
	URL string `json:"url"`
}

// ZarfClusterFiles defines a file to write to a host.
type ZarfClusterFiles struct {
	// Name identifies the file in the cluster configuration.
	Name string `json:"name"`
	// Source is the local path or URL cargoship reads the file from.
	Source string `json:"src,omitempty"`
	// Destination is the path on the host where cargoship writes the file.
	Destination string `json:"dst,omitempty"`
	// DestinationDirectory is the directory on the host where cargoship writes the file.
	DestinationDirectory string `json:"dstDir,omitempty"`
	// Permission sets the file mode cargoship applies on the host.
	Permission string `json:"perm,omitempty"`
	// User identifies the file owner on the host.
	User string `json:"user,omitempty" jsonschema:"example=root"`
	// Group identifies the file group on the host.
	Group string `json:"group,omitempty" jsonschema:"example=root"`
	// Data holds inline content for the file, as an alternative to Source.
	Data string `json:"data,omitempty"`
}
