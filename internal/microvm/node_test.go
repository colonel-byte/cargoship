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
	"strings"
	"testing"

	apicluster "github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/stretchr/testify/require"
)

func TestSpecNormalizeDefaultsAndRejections(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		spec    Spec
		wantErr string
		want    Spec
	}{
		{
			name: "defaults fill in around a controller count",
			spec: Spec{
				Controllers: 1,
			},
			want: Spec{
				Fleet:       "dev",
				Controllers: 1,
				Workers:     0,
				MemoryMiB:   4096,
				CPUs:        2,
				DiskSize:    "20G",
				Distro:      "k3s",
			},
		},
		{
			name: "explicit values are left alone",
			spec: Spec{
				Fleet:       "probe",
				Controllers: 3,
				Workers:     2,
				MemoryMiB:   2048,
				CPUs:        4,
				DiskSize:    "40G",
				Distro:      "rke2",
			},
			want: Spec{
				Fleet:       "probe",
				Controllers: 3,
				Workers:     2,
				MemoryMiB:   2048,
				CPUs:        4,
				DiskSize:    "40G",
				Distro:      "rke2",
			},
		},
		{
			name: "no controller",
			spec: Spec{
				Controllers: 0,
			},
			wantErr: "at least one controller",
		},
		{
			name: "negative workers",
			spec: Spec{
				Controllers: 1,
				Workers:     -1,
			},
			wantErr: "cannot have -1 workers",
		},
		{
			name: "past the node cap",
			spec: Spec{
				Controllers: 1,
				Workers:     maxNodes,
			},
			wantErr: "exceeds the 32 node limit",
		},
		{
			name: "unknown distro",
			spec: Spec{
				Controllers: 1,
				Distro:      "openshift",
			},
			wantErr: "must be k3s or rke2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.spec.Normalize()
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestSpecNodesNamesAndRoles pins the names, because they are not free to change: the e2e
// suite maps a hostname prefix onto a role, and the same two prefixes have to claim a fleet
// node and a bootloose machine.
func TestSpecNodesNamesAndRoles(t *testing.T) {
	t.Parallel()

	nodes, err := Spec{
		Fleet:       "dev",
		Controllers: 2,
		Workers:     3,
	}.Nodes()
	require.NoError(t, err)

	var gotNames []string
	for _, n := range nodes {
		gotNames = append(gotNames, n.Name)
	}
	require.Equal(t, []string{"kc0", "kc1", "kw0", "kw1", "kw2"}, gotNames,
		"controllers come first so that index 1 is the leader")

	for i, n := range nodes {
		require.Equal(t, i+1, n.Index, "indexes are 1-based and run across both roles")
		if strings.HasPrefix(n.Name, controllerPrefix) {
			require.Equal(t, apicluster.RoleController, n.Role)
		} else {
			require.Equal(t, apicluster.RoleWorker, n.Role)
		}
	}
}

// TestSpecNodesDerivationIsUnique is the property every other command depends on: nothing
// per-node is stored, so Down, List and SSH recompute what Up used. Two nodes colliding on a
// port, an address or a MAC would make a fleet that half works.
func TestSpecNodesDerivationIsUnique(t *testing.T) {
	t.Parallel()

	nodes, err := Spec{
		Fleet:       "dev",
		Controllers: 3,
		Workers:     5,
	}.Nodes()
	require.NoError(t, err)

	ports := map[int]string{}
	addrs := map[string]string{}
	macs := map[string]string{}
	for _, n := range nodes {
		require.NotContains(t, ports, n.SSHPort, "ssh port reused")
		ports[n.SSHPort] = n.Name
		require.NotContains(t, addrs, n.PrivateAddress, "private address reused")
		addrs[n.PrivateAddress] = n.Name

		for _, mac := range []string{n.MgmtMAC, n.LANMAC} {
			require.NotContains(t, macs, mac, "mac reused")
			macs[mac] = n.Name
		}

		for _, f := range n.HostFwd {
			require.NotContains(t, ports, f.HostPort, "forward collides with an ssh port")
			ports[f.HostPort] = n.Name
		}
	}
}

// TestSpecNodesSeparatesFleets covers running two fleets side by side, which only works if
// nothing about one is derivable from the node index alone.
func TestSpecNodesSeparatesFleets(t *testing.T) {
	t.Parallel()

	one, err := Spec{
		Fleet:       "dev",
		Controllers: 1,
		Workers:     1,
	}.Nodes()
	require.NoError(t, err)

	two, err := Spec{
		Fleet:       "other",
		Controllers: 1,
		Workers:     1,
	}.Nodes()
	require.NoError(t, err)

	require.NotEqual(t, fleetID("dev"), fleetID("other"), "the fixture relies on these differing")

	for i := range one {
		require.NotEqual(t, one[i].SSHPort, two[i].SSHPort)
		require.NotEqual(t, one[i].PrivateAddress, two[i].PrivateAddress)
		require.NotEqual(t, one[i].MgmtMAC, two[i].MgmtMAC)
		require.NotEqual(t, one[i].LANMAC, two[i].LANMAC)
	}
	require.NotEqual(t, mcastFor(fleetID("dev")), mcastFor(fleetID("other")),
		"two fleets sharing a multicast group would share a broadcast domain")
}

// TestLeaderForwards covers the one place the engine choice reaches the command line: rke2
// serves its join endpoint on a port of its own, and k3s does not.
func TestLeaderForwards(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		distro         string
		wantGuestPorts []int
	}{
		{
			name:           "k3s multiplexes onto the api port",
			distro:         "k3s",
			wantGuestPorts: []int{6443},
		},
		{
			name:           "rke2 adds its join endpoint",
			distro:         "rke2",
			wantGuestPorts: []int{6443, 9345},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			nodes, err := Spec{
				Fleet:       "dev",
				Controllers: 2,
				Workers:     1,
				Distro:      tt.distro,
			}.Nodes()
			require.NoError(t, err)

			var got []int
			for _, f := range nodes[0].HostFwd {
				got = append(got, f.GuestPort)
			}
			require.Equal(t, tt.wantGuestPorts, got)

			for _, n := range nodes[1:] {
				require.Empty(t, n.HostFwd, "only the leader forwards engine ports")
			}
		})
	}
}
