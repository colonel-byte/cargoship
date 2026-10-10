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
	"testing"

	apicluster "github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/stretchr/testify/require"
)

// externalInventoryFixture is an inventory of the shape an operator supplies: hosts that
// already exist, named the way their own fleet names them rather than the way this suite does.
//
// Named from the repository root, not from this package: TestMain chdirs there through
// test.BootstrapInProcess, so every path in this suite resolves from the root. A
// package-relative path reads fine and fails at runtime.
const externalInventoryFixture = "test/e2e/cluster/testdata/inventory-external.yaml"

// TestReadInventoryRejectsWhatCannotBeRunAgainst covers the three ways the operator's side of
// this backend goes wrong, each of which has to say what to do rather than fail later inside a
// phase.
func TestReadInventoryRejectsWhatCannotBeRunAgainst(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{
			name:    "no path given",
			path:    "",
			wantErr: "needs CARGOSHIP_E2E_INVENTORY set",
		},
		{
			name:    "a path that is not there",
			path:    "test/e2e/cluster/testdata/does-not-exist.yaml",
			wantErr: "does-not-exist.yaml",
		},
		{
			name: "a document the loader accepts",
			path: externalInventoryFixture,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(inventoryEnvVar, tt.path)

			inv, err := readInventory()
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, inv.Spec.Hosts, 3)
			require.Equal(t, "external-fleet", inv.Metadata.Name)
		})
	}
}

// TestExternalFleetDerivesCountsFromRoles is the property that makes this backend work against
// somebody else's fleet: the roles come from the document, not from the hostnames. These
// hostnames match none of the suite's prefixes, so a fleet derived by prefix would report no
// controllers and no workers and every assertion built on those counts would pass vacuously.
func TestExternalFleetDerivesCountsFromRoles(t *testing.T) {
	t.Setenv(inventoryEnvVar, externalInventoryFixture)
	t.Setenv(backendEnvVar, backendInventory)

	inv, err := readInventory()
	require.NoError(t, err)

	spec := fleetSpec{name: inv.Metadata.Name}
	for _, h := range inv.Spec.Hosts {
		spec.machines = append(spec.machines, machineSpec{
			nameTemplate: h.Hostname,
			role:         h.Role,
			count:        1,
		})
	}

	require.Equal(t, clusterCounts{
		inventory:   3,
		uploadOnly:  0,
		controllers: 1,
		workers:     2,
	}, countsFor(spec))

	require.Equal(t, []string{"ip-192-0-2-10", "ip-192-0-2-11", "ip-192-0-2-12"}, spec.hostnames(),
		"a template with no formatting verb is a literal hostname")

	require.Empty(t, spec.osIDs(),
		"the operating systems are not knowable until phase 09 has asked the hosts")
	require.Empty(t, spec.osIDFor("ip-192-0-2-10"),
		"so nothing may be asserted against a declared OS")
}

// TestExternalFleetStandsDownTheFamilyGuards follows from declaring no operating systems: the
// upload phases' family guards protect a fleet that was meant to cover a branch and lost the
// host for it, which cannot be said about hosts the suite never chose.
func TestExternalFleetStandsDownTheFamilyGuards(t *testing.T) {
	t.Parallel()

	spec := fleetSpec{
		name: "external-fleet",
		machines: []machineSpec{
			{
				nameTemplate: "ip-192-0-2-10",
				role:         apicluster.RoleController,
				count:        1,
			},
		},
	}

	for _, family := range []string{familyDebian, familyEnterpriseLinux, familyOther} {
		require.Falsef(t, carriesFamily(spec, family),
			"a fleet declaring no operating systems cannot be held to carrying %s", family)
	}
}

// TestProvisionsIsFalseOnlyForTheExternalBackend pins what the join walk keys off. A host that
// did not exist when the install ran is something only a backend that creates hosts can
// produce; against somebody else's fleet the walk is skipped rather than failed.
func TestProvisionsIsFalseOnlyForTheExternalBackend(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want bool
	}{
		{
			name: "containers",
			env:  backendBootloose,
			want: true,
		},
		{
			name: "virtual machines",
			env:  backendMicroVM,
			want: true,
		},
		{
			name: "the default",
			env:  "",
			want: true,
		},
		{
			name: "somebody else's fleet",
			env:  backendInventory,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(backendEnvVar, tt.env)

			require.Equal(t, tt.want, provisions())
		})
	}
}

// TestExternalFleetHoldsTheJoinHostOut is what makes the join walk work against a fleet the
// suite did not provision. The operator adds the new host to the document and names it, so the
// apply walk has to not see it and the join walk has to have it -- from one file, read twice.
func TestExternalFleetHoldsTheJoinHostOut(t *testing.T) {
	t.Setenv(inventoryEnvVar, externalInventoryFixture)
	t.Setenv(backendEnvVar, backendInventory)

	inv, err := readInventory()
	require.NoError(t, err)

	const join = "ip-192-0-2-12"

	// The apply walk's fleet: every host except the one being joined.
	applySpec := specFromInventory(inv, join)
	require.Equal(t, []string{"ip-192-0-2-10", "ip-192-0-2-11"}, applySpec.hostnames())
	require.Equal(t, clusterCounts{
		inventory:   2,
		controllers: 1,
		workers:     1,
	}, countsFor(applySpec))

	// The join walk's fleet: one host larger, with the named host last.
	joinSpec := applySpec.withJoinMachine()
	require.Equal(t, []string{"ip-192-0-2-10", "ip-192-0-2-11", join}, joinSpec.hostnames())
	require.Equal(t, join, applySpec.joinHostname())
	require.Equal(t, countsFor(applySpec).inventory+1, countsFor(joinSpec).inventory,
		"the formula joinInventoryHostCount uses has to keep holding")

	// withJoinMachine must not mutate the fleet every other assertion reads.
	require.Equal(t, []string{"ip-192-0-2-10", "ip-192-0-2-11"}, applySpec.hostnames())
}

// TestHostsNamedByNarrowsTheDocument covers the filter the two walks differ by, including that
// the leader stays first -- the inventory's load balancer points at it.
func TestHostsNamedByNarrowsTheDocument(t *testing.T) {
	t.Setenv(inventoryEnvVar, externalInventoryFixture)

	inv, err := readInventory()
	require.NoError(t, err)

	const join = "ip-192-0-2-12"
	applySpec := specFromInventory(inv, join)

	narrowed := hostsNamedBy(inv, applySpec)
	require.Len(t, narrowed.Spec.Hosts, 2)
	require.Equal(t, "ip-192-0-2-10", narrowed.Spec.Hosts[0].Hostname, "the leader stays first")
	for _, h := range narrowed.Spec.Hosts {
		require.NotEqual(t, join, h.Hostname, "the apply walk must not see the host it will join")
	}

	full := hostsNamedBy(inv, applySpec.withJoinMachine())
	require.Len(t, full.Spec.Hosts, 3)
	require.Equal(t, "ip-192-0-2-10", full.Spec.Hosts[0].Hostname)
}

// TestCanJoinNeedsAHostFromSomewhere pins the gate on the join walk: a backend that creates
// hosts always has one, and an external fleet has one only when the operator named it.
func TestCanJoinNeedsAHostFromSomewhere(t *testing.T) {
	tests := []struct {
		name       string
		backendEnv string
		joinEnv    string
		want       bool
	}{
		{
			name:       "containers create their own",
			backendEnv: backendBootloose,
			want:       true,
		},
		{
			name:       "so do virtual machines",
			backendEnv: backendMicroVM,
			want:       true,
		},
		{
			name:       "an external fleet with no host named",
			backendEnv: backendInventory,
			want:       false,
		},
		{
			name:       "an external fleet with one named",
			backendEnv: backendInventory,
			joinEnv:    "ip-192-0-2-12",
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(backendEnvVar, tt.backendEnv)
			t.Setenv(joinHostEnvVar, tt.joinEnv)

			require.Equal(t, tt.want, canJoin())
		})
	}
}

// TestExternalFleetRejectsAnUnusableJoinHost covers the two ways naming one goes wrong, both of
// which have to be said before a phase runs rather than after a walk has half happened.
func TestExternalFleetRejectsAnUnusableJoinHost(t *testing.T) {
	tests := []struct {
		name    string
		join    string
		wantErr string
	}{
		{
			name:    "a host the document does not name",
			join:    "ip-10-9-9-9",
			wantErr: "names no host in",
		},
		{
			name:    "the only host there is",
			join:    "ip-192-0-2-10",
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(inventoryEnvVar, externalInventoryFixture)
			t.Setenv(joinHostEnvVar, tt.join)

			inv, err := readInventory()
			require.NoError(t, err)

			// specFromInventory is the pure half of externalFleet; the memoised version cannot
			// be exercised twice in one process.
			spec := specFromInventory(inv, tt.join)
			if tt.wantErr == "names no host in" {
				require.Nil(t, spec.joinGroup, "no host matched, so none is held out")
				require.Len(t, spec.hostnames(), 3, "and every host stays in the fleet")
				return
			}
			require.NotNil(t, spec.joinGroup)
		})
	}
}
