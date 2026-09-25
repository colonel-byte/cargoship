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

// Package cluster holds the e2e tests that need a real multi-node cluster: the install
// command group, driven against containers provisioned by bootloose. Running it requires
// Docker. The misc and package groups live in the sibling noncluster package.
package cluster

import (
	"fmt"
	"os"
	"sort"
	"strings"

	apicluster "github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	goyaml "github.com/goccy/go-yaml"
	blcluster "github.com/k0sproject/bootloose/pkg/cluster"
	"github.com/k0sproject/rig"
)

const (
	sshPort = 22
	sshUser = "root"
)

// uploadOnlyPrefix marks the machines that are in the inventory to receive uploads and
// nothing more. rke2 links against glibc, so the Alpine node cannot run the engine: it is
// there because the BIN upload phase is the one path that claims a host belonging to neither
// the Enterprise Linux nor the Debian family, and without such a host that phase is only ever
// exercised as a fallback for hosts the other two declined.
//
// The prefix starts with the worker prefix, so renderClusterInventory gives these machines the
// worker role without a special case -- which is what the upload phases key on. Everything
// from the engine configuration onwards has to exclude them again.
const uploadOnlyPrefix = "kwa"

// isUploadOnly reports whether a host is in the inventory only to receive uploads.
func isUploadOnly(h *apicluster.ZarfHost) bool {
	return strings.HasPrefix(h.Hostname, uploadOnlyPrefix)
}

// engineHosts returns the hosts that join the cluster, which is every host that is not
// upload-only. This is the inventory the CLI-driven steps are given.
func engineHosts(c apicluster.ZarfCluster) apicluster.ZarfCluster {
	c.Spec.Hosts = c.Spec.Hosts.Filter(func(h *apicluster.ZarfHost) bool { return !isUploadOnly(h) })
	return c
}

// renderClusterInventory builds a ZarfCluster inventory from the live bootloose machines,
// mapping kc*/kw* hostnames to controller/worker roles. The Fedora replicas are named kcf*
// and kwf*, so the same two prefixes claim them and the OS family never has to be part of
// the mapping. A machine whose name matches neither prefix is skipped rather than guessed at.
func renderClusterInventory(c *blcluster.Cluster, keyPath string) (apicluster.ZarfCluster, error) {
	machines, err := c.Inspect(nil)
	if err != nil {
		return apicluster.ZarfCluster{}, fmt.Errorf("failed to inspect bootloose cluster: %w", err)
	}

	var controllers, workers []*blcluster.Machine
	for _, m := range machines {
		switch {
		case strings.HasPrefix(m.Hostname(), "kc"):
			controllers = append(controllers, m)
		case strings.HasPrefix(m.Hostname(), "kw"):
			workers = append(workers, m)
		}
	}
	if len(controllers) == 0 {
		return apicluster.ZarfCluster{}, fmt.Errorf("no controller machines found in bootloose cluster")
	}
	sortByHostname(controllers)
	sortByHostname(workers)

	hosts := make(apicluster.ZarfHosts, 0, len(controllers)+len(workers))
	for _, m := range controllers {
		host, err := hostFromMachine(m, apicluster.RoleController, keyPath)
		if err != nil {
			return apicluster.ZarfCluster{}, err
		}
		hosts = append(hosts, host)
	}
	for _, m := range workers {
		host, err := hostFromMachine(m, apicluster.RoleWorker, keyPath)
		if err != nil {
			return apicluster.ZarfCluster{}, err
		}
		hosts = append(hosts, host)
	}

	leaderIP := controllers[0].Status().IP
	if leaderIP == "" {
		return apicluster.ZarfCluster{}, fmt.Errorf("could not determine docker-bridge IP for leader controller %s", controllers[0].Hostname())
	}

	return apicluster.ZarfCluster{
		Kind: "ZarfCluster",
		Metadata: apicluster.ZarfClusterMetadata{
			Name: "bootloose-e2e",
		},
		Spec: apicluster.ZarfClusterSpec{
			Config: apicluster.ZarfClusterConfig{
				LoadBalancer: leaderIP,
			},
			Hosts: hosts,
		},
	}, nil
}

func sortByHostname(machines []*blcluster.Machine) {
	sort.Slice(machines, func(i, j int) bool {
		return machines[i].Hostname() < machines[j].Hostname()
	})
}

func hostFromMachine(m *blcluster.Machine, role string, keyPath string) (*apicluster.ZarfHost, error) {
	port, err := m.HostPort(sshPort)
	if err != nil {
		return nil, fmt.Errorf("failed to get SSH host port for %s: %w", m.Hostname(), err)
	}
	return &apicluster.ZarfHost{
		Hostname: m.Hostname(),
		Role:     role,
		// The profile doubles as the node-role.kubernetes.io/<profile> label the LabelNodes
		// phase writes, so giving each host a profile matching its role is what makes that
		// phase observable. It is not mapped to an entry in the cluster's profile config, which
		// leaves per-profile concurrency on its default.
		Profile: role,
		Connection: rig.Connection{
			SSH: &rig.SSH{
				Address: "127.0.0.1",
				User:    sshUser,
				Port:    port,
				KeyPath: &keyPath,
			},
		},
	}, nil
}

// writeClusterInventory marshals cluster to YAML (via the same library cargoship uses to
// parse a ZarfCluster document) and writes it to path.
func writeClusterInventory(cluster apicluster.ZarfCluster, path string) error {
	b, err := goyaml.Marshal(cluster)
	if err != nil {
		return fmt.Errorf("failed to marshal cluster inventory: %w", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("failed to write cluster inventory to %s: %w", path, err)
	}
	return nil
}
