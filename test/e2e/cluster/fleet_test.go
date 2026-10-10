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

	"github.com/stretchr/testify/require"
)

// TestCountsForMatchesTheShippedFleets pins the counts the suite's assertions are built on. A
// spec and the counts read off it are the two things every phase test compares, so a change to
// either that does not match the other turns real assertions into vacuous ones.
func TestCountsForMatchesTheShippedFleets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec fleetSpec
		want clusterCounts
	}{
		{
			name: "the staging fleet, one machine per family per role plus the alpine node",
			spec: stageOS,
			want: clusterCounts{
				inventory:   5,
				uploadOnly:  1,
				controllers: 2,
				workers:     2,
			},
		},
		{
			name: "the k3s fleet, one controller and four workers across two images",
			spec: k3sOS,
			want: clusterCounts{
				inventory:   5,
				uploadOnly:  0,
				controllers: 1,
				workers:     4,
			},
		},
		{
			name: "the k3s fleet once the join walk has grown it",
			spec: k3sOS.withJoinMachine(),
			want: clusterCounts{
				inventory:   6,
				uploadOnly:  0,
				controllers: 1,
				workers:     5,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, countsFor(tt.spec))
		})
	}
}

// TestHostnamesAreDistinct covers the reason withJoinMachine raises a count instead of adding
// a replica group: a second group formats its own template from index zero again, so it would
// name a machine that already exists.
func TestHostnamesAreDistinct(t *testing.T) {
	t.Parallel()

	for _, spec := range []fleetSpec{stageOS, k3sOS, k3sOS.withJoinMachine(), stageOS.withJoinMachine()} {
		seen := map[string]bool{}
		for _, name := range spec.hostnames() {
			require.False(t, seen[name], "two machines named %s", name)
			seen[name] = true
		}
	}
}

// TestWithJoinMachineAddsTheFedoraWorker names the machine the join walk expects. A Fedora
// worker is deliberate: the new node then comes through the SELinux, fapolicyd, firewalld and
// dnf branches on its own rather than inheriting what the apply walk proved beside it.
func TestWithJoinMachineAddsTheFedoraWorker(t *testing.T) {
	t.Parallel()

	before := k3sOS.hostnames()
	after := k3sOS.withJoinMachine().hostnames()

	require.Len(t, after, len(before)+1)
	require.Subset(t, after, before)
	require.Contains(t, after, k3sOS.joinHostname())
	require.NotContains(t, before, k3sOS.joinHostname())

	require.Equal(t, before, k3sOS.hostnames(),
		"withJoinMachine copies the replica groups rather than mutating the shared fleet")
}

// TestBootlooseConfigMirrorsTheSpec covers the one backend that exists so far: every replica
// group reaches the bootloose config with its name, image and count intact.
func TestBootlooseConfigMirrorsTheSpec(t *testing.T) {
	t.Parallel()

	cfg := stageOS.bootlooseConfig()

	require.Equal(t, stageOS.name, cfg.Cluster.Name)
	require.Equal(t, "cluster-key", cfg.Cluster.PrivateKey)
	require.Len(t, cfg.Machines, len(stageOS.machines))

	for i, m := range cfg.Machines {
		require.Equal(t, stageOS.machines[i].count, m.Count)
		require.Equal(t, stageOS.machines[i].nameTemplate, m.Spec.Name)
		require.Equal(t, stageOS.machines[i].image, m.Spec.Image)
		require.True(t, m.Spec.Privileged, "the prepare phase's sysctl --system needs it")
		require.NotEmpty(t, m.Spec.Volumes, "the engine's data directory needs a volume, see engineData")
	}
}

// TestOsIDForPrefersTheLongestPrefix covers the reason this is a longest-match walk: the name
// templates nest, so "kw" is a prefix of both "kwf" and "kwa" and a shortest-match walk would
// report every Fedora and Alpine worker as Ubuntu.
func TestOsIDForPrefersTheLongestPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		hostname string
		want     string
	}{
		{
			name:     "ubuntu controller",
			hostname: "kc0",
			want:     "ubuntu",
		},
		{
			name:     "fedora controller, whose prefix extends the ubuntu one",
			hostname: "kcf0",
			want:     "fedora",
		},
		{
			name:     "ubuntu worker",
			hostname: "kw0",
			want:     "ubuntu",
		},
		{
			name:     "fedora worker, whose prefix extends the ubuntu one",
			hostname: "kwf0",
			want:     "fedora",
		},
		{
			name:     "the upload-only alpine worker",
			hostname: "kwa0",
			want:     "alpine",
		},
		{
			name:     "a name no replica group produces",
			hostname: "zz0",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, stageOS.osIDFor(tt.hostname))
		})
	}
}

// TestOsIDsCoversOnlyWhatTheFleetProvisions is what stops the OS-mix assertion in phase 09
// from requiring a family the run never asked for.
func TestOsIDsCoversOnlyWhatTheFleetProvisions(t *testing.T) {
	t.Parallel()

	require.Equal(t, map[string]struct{}{
		"ubuntu": {},
		"fedora": {},
		"alpine": {},
	}, stageOS.osIDs(), "the staging fleet runs one machine per family")

	require.Equal(t, map[string]struct{}{
		"ubuntu": {},
		"fedora": {},
	}, k3sOS.osIDs(), "the k3s fleet has no alpine host, so nothing may require one")
}

// TestSameVersionSpansBothNotations is the comparison every engine-version assertion in this
// suite goes through. A package declares "1.36.4-k3s1" and the engine's own --version reports
// "v1.36.4+k3s1"; those name one version, and comparing them as strings fails a correct
// install. This is what that failure looked like before the microvm backend made the engine
// phases reachable.
func TestSameVersionSpansBothNotations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		want    string
		got     string
		same    bool
		wantErr string
	}{
		{
			name: "k3s package notation against the engine's own",
			want: "1.36.4-k3s1",
			got:  "v1.36.4+k3s1",
			same: true,
		},
		{
			name: "rke2 package notation against the engine's own",
			want: "1.36.4-rke2r1",
			got:  "v1.36.4+rke2r1",
			same: true,
		},
		{
			name: "identical strings",
			want: "1.36.4-k3s1",
			got:  "1.36.4-k3s1",
			same: true,
		},
		{
			name: "a different patch is not the same version",
			want: "1.36.4-k3s1",
			got:  "v1.36.5+k3s1",
			same: false,
		},
		{
			name: "a different build of the same patch is not the same version",
			want: "1.36.4-k3s1",
			got:  "v1.36.4+k3s2",
			same: false,
		},
		{
			name:    "the phase reported no version at all",
			want:    "1.36.4-k3s1",
			got:     "unknown",
			wantErr: "parsing the reported version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			same, err := sameVersion(tt.want, tt.got)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.same, same)
		})
	}
}
