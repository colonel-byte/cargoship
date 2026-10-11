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

package microvm

import (
	"testing"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1"
	apicluster "github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	goyaml "github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

// fleetFixture is a fleet as Up would have produced it, without starting anything.
func fleetFixture(t *testing.T) Fleet {
	t.Helper()

	spec, err := Spec{
		Fleet:       "dev",
		Controllers: 2,
		Workers:     2,
		Infra:       2,
		Distro:      "rke2",
	}.Normalize()
	require.NoError(t, err)

	nodes, err := spec.Nodes()
	require.NoError(t, err)

	return Fleet{
		Name:    spec.Fleet,
		Dir:     spec.dir(),
		KeyPath: "/run/fleet/dev/key",
		Nodes:   nodes,
		Distro:  spec.Distro,
	}
}

func TestInventoryHosts(t *testing.T) {
	t.Parallel()

	f := fleetFixture(t)
	inv := Inventory(f)

	require.Equal(t, v1alpha1.ZarfDistroKind("ZarfCluster"), inv.Kind)
	require.Equal(t, "microvm-dev", inv.Metadata.Name)
	require.Len(t, inv.Spec.Hosts, len(f.Nodes))

	for i, host := range inv.Spec.Hosts {
		node := f.Nodes[i]
		require.Equal(t, node.Name, host.Hostname)
		require.Equal(t, node.Role, host.Role)
		require.Equal(t, node.Profile, host.Profile,
			"the profile is what the LabelNodes phase writes as a node role label")

		require.NotNil(t, host.ConnectionConfig.SSH)
		require.Equal(t, "127.0.0.1", host.ConnectionConfig.SSH.Address)
		require.Equal(t, sshUser, host.ConnectionConfig.SSH.User)
		require.Equal(t, node.SSHPort, host.ConnectionConfig.SSH.Port)
		require.NotNil(t, host.ConnectionConfig.SSH.KeyPath)
		require.Equal(t, f.KeyPath, *host.ConnectionConfig.SSH.KeyPath)
	}
}

// TestInventoryPinsThePrivateSegment is the assertion worth having here. A node's default
// route is on the slirp management interface, so leaving these to fact gathering would give
// every host the same 10.0.2.15 -- an address no peer can reach -- and the firewall and hosts
// phases would distribute it.
func TestInventoryPinsThePrivateSegment(t *testing.T) {
	t.Parallel()

	f := fleetFixture(t)
	inv := Inventory(f)

	seen := map[string]bool{}
	for i, host := range inv.Spec.Hosts {
		require.Equal(t, f.Nodes[i].PrivateAddress, host.PrivateAddress)
		require.Equal(t, lanInterface, host.PrivateInterface)

		require.NotEqual(t, "10.0.2.15", host.PrivateAddress,
			"that is the slirp address, which is identical on every node")
		require.False(t, seen[host.PrivateAddress], "two hosts share a private address")
		seen[host.PrivateAddress] = true
	}

	require.Equal(t, f.Nodes[0].PrivateAddress, inv.Spec.Config.LoadBalancer,
		"workers join the leader over the private segment, not through a forwarded host port")
}

// TestInventoryRoundTripsThroughLoad covers the only claim that matters about the written
// document: cargoship has to be able to read it back. It is marshalled here and parsed with
// the same library the CLI parses an inventory with, so a field that marshals to a shape the
// loader rejects fails here rather than at the connect phase.
func TestInventoryRoundTripsThroughLoad(t *testing.T) {
	t.Parallel()

	f := fleetFixture(t)

	b, err := goyaml.Marshal(Inventory(f))
	require.NoError(t, err)

	var got apicluster.ZarfCluster
	require.NoError(t, goyaml.Unmarshal(b, &got))

	require.Len(t, got.Spec.Hosts, len(f.Nodes))
	require.Equal(t, f.Nodes[0].PrivateAddress, got.Spec.Config.LoadBalancer)
	for i, host := range got.Spec.Hosts {
		require.Equal(t, f.Nodes[i].Name, host.Hostname)
		require.Equal(t, f.Nodes[i].Role, host.Role)
		require.Equal(t, f.Nodes[i].PrivateAddress, host.PrivateAddress)
		require.Equal(t, lanInterface, host.PrivateInterface)

		require.NotNil(t, host.ConnectionConfig.SSH, "the ssh block did not survive the round trip")
		require.Equal(t, f.Nodes[i].SSHPort, host.ConnectionConfig.SSH.Port)
	}
}

// TestInventoryGivesInfraNodesTheirOwnProfile is why the infra group exists. Everything else
// in this inventory has a profile that restates its role, so the node-role labelling and the
// per-profile concurrency are only exercised against a profile that differs.
func TestInventoryGivesInfraNodesTheirOwnProfile(t *testing.T) {
	t.Parallel()

	inv := Inventory(fleetFixture(t))

	var infra, other int
	for _, host := range inv.Spec.Hosts {
		if host.Profile == infraProfile {
			infra++
			require.Equal(t, apicluster.RoleWorker, host.Role,
				"an infra node joins as a worker; the engine has no third role")
			continue
		}
		other++
		require.Equal(t, host.Role, host.Profile)
	}

	require.Equal(t, 2, infra, "the fixture has two infra nodes")
	require.Positive(t, other)

	require.NotEqual(t, infraProfile, inv.Spec.Hosts[0].Profile,
		"the leader is a controller, and the load balancer points at it")
}
