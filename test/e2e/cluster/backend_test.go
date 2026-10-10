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
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBackendSelection(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		want    string
		wantErr string
	}{
		{
			name: "unset defaults to containers",
			env:  "",
			want: backendBootloose,
		},
		{
			name: "named explicitly",
			env:  backendBootloose,
			want: backendBootloose,
		},
		{
			name: "virtual machines",
			env:  backendMicroVM,
			want: backendMicroVM,
		},
		{
			name:    "a typo is an error, not the default",
			env:     "microvms",
			wantErr: "is not a backend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(backendEnvVar, tt.env)

			got, err := backend()
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr,
					"a misspelt backend must not silently fall back to the default, since setting it at all means wanting the other one")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestClusterConfigFollowsTheBackend covers the fleet each backend provisions. The VM fleet
// deliberately has no upload-only node: that machine exists because rke2 is glibc-only and the
// Alpine image is musl, and Rocky is neither of those things.
func TestClusterConfigFollowsTheBackend(t *testing.T) {
	tests := []struct {
		name       string
		backendEnv string
		stageOnly  string
		want       fleetSpec
	}{
		{
			name:       "containers, full walk",
			backendEnv: backendBootloose,
			want:       k3sOS,
		},
		{
			name:       "containers, stage only",
			backendEnv: backendBootloose,
			stageOnly:  "1",
			want:       stageOS,
		},
		{
			name:       "virtual machines",
			backendEnv: backendMicroVM,
			want:       vmOS,
		},
		{
			name:       "virtual machines, stage only, which changes the walk and not the fleet",
			backendEnv: backendMicroVM,
			stageOnly:  "1",
			want:       vmOS,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(backendEnvVar, tt.backendEnv)
			t.Setenv(stageOnlyEnvVar, tt.stageOnly)

			require.Equal(t, tt.want, clusterConfig())
		})
	}
}

// TestVMFleetShape pins what the VM fleet is for. A single distribution is the point, and the
// counts are what microvmSpec hands the backend.
func TestVMFleetShape(t *testing.T) {
	t.Parallel()

	require.Equal(t, map[string]struct{}{"rocky": {}}, vmOS.osIDs(),
		"the VM backend has one image, pinned in internal/microvm")

	require.Equal(t, clusterCounts{
		inventory:   3,
		uploadOnly:  0,
		controllers: 1,
		workers:     2,
	}, countsFor(vmOS))

	spec := microvmSpec(vmOS)
	require.Equal(t, 1, spec.Controllers)
	require.Equal(t, 2, spec.Workers)

	require.Equal(t, "kw2", vmOS.joinHostname(),
		"the join walk grows the only worker group, since there is no second one to contrast with")
}

// TestCarriesFamily is what keeps the family-specific upload guards honest: a fleet meant to
// cover a branch still fails loudly when it loses the host for it, and a fleet that never
// declared one skips instead.
func TestCarriesFamily(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		spec   fleetSpec
		family string
		want   bool
	}{
		{
			name:   "the container fleet carries debian",
			spec:   k3sOS,
			family: familyDebian,
			want:   true,
		},
		{
			name:   "the container fleet carries enterprise linux, via fedora",
			spec:   k3sOS,
			family: familyEnterpriseLinux,
			want:   true,
		},
		{
			name:   "the staging fleet carries a host in neither family, which is the BIN path",
			spec:   stageOS,
			family: familyOther,
			want:   true,
		},
		{
			name:   "the k3s fleet carries no such host, having no alpine node",
			spec:   k3sOS,
			family: familyOther,
			want:   false,
		},
		{
			name:   "the VM fleet is enterprise linux",
			spec:   vmOS,
			family: familyEnterpriseLinux,
			want:   true,
		},
		{
			name:   "the VM fleet carries no debian host, so the APT guard must not fire",
			spec:   vmOS,
			family: familyDebian,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, carriesFamily(tt.spec, tt.family))
		})
	}
}

// TestReachesClusterAPIOnAnAddressNothingListensOn covers the negative side of the probe, which
// is the side that decides whether a step runs. The documentation-range address is reserved and
// routed nowhere, so this cannot pass by accident on a machine that happens to run a cluster.
func TestReachesClusterAPIOnAnAddressNothingListensOn(t *testing.T) {
	t.Parallel()

	require.False(t, reachesClusterAPI(""),
		"an inventory with no load balancer names nothing to reach")
	require.False(t, reachesClusterAPI("192.0.2.1"),
		"a reserved documentation address is routed nowhere")
}

// TestReachesClusterAPIFindsAListener is the positive side, against a listener this test owns
// rather than a cluster, so it asserts the probe and nothing about the environment.
func TestReachesClusterAPIFindsAListener(t *testing.T) {
	t.Parallel()

	// The probe always dials 6443, so the listener has to be on that port. Skip rather than
	// fail when it is already taken: a developer with a local cluster is not a broken test.
	ln, err := net.Listen("tcp", "127.0.0.1:6443")
	if err != nil {
		t.Skipf("cannot bind 127.0.0.1:6443 to test the probe: %v", err)
	}
	defer func() { _ = ln.Close() }()

	require.True(t, reachesClusterAPI("127.0.0.1"))
}
