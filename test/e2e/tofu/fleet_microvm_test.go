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

package tofu

import (
	"testing"

	"github.com/colonel-byte/cargoship/test/microvm"
	"github.com/stretchr/testify/require"
)

// TestSelectedBackend covers the one thing a typo in the variable has to do: stop the run
// naming what it was given, rather than quietly provisioning the default.
func TestSelectedBackend(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{
			name:  "unset is containers",
			value: "",
			want:  backendBootloose,
		},
		{
			name:  "bootloose by name",
			value: backendBootloose,
			want:  backendBootloose,
		},
		{
			name:  "microvm by name",
			value: backendMicroVM,
			want:  backendMicroVM,
		},
		{
			name:    "a typo is not the default",
			value:   "microvms",
			wantErr: true,
		},
		{
			name:    "the cluster suite's third backend is not one of ours",
			value:   "inventory",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(backendEnvVar, tc.value)

			got, err := selectedBackend()
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.value, "the error does not name what it was given")
				require.False(t, onMicroVM(), "a value that is not a backend read as the VM one")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.want == backendMicroVM, onMicroVM())
		})
	}
}

// TestVMFleetVars checks the mapping the walk depends on: the private addressing reaches the
// module, and the load balancer is the leader's address on the fleet's own segment rather than
// the loopback port the suite reaches it through.
func TestVMFleetVars(t *testing.T) {
	key := "/tmp/key"
	fleet := microvm.Fleet{
		Name:    "tofu",
		KeyPath: key,
		Nodes: []microvm.Node{
			{
				Name:           "kc0",
				Role:           "controller",
				Profile:        "controller",
				Index:          1,
				SSHPort:        26241,
				PrivateAddress: "10.73.156.11",
			},
			{
				Name:           "kw0",
				Role:           "worker",
				Profile:        "worker",
				Index:          2,
				SSHPort:        26242,
				PrivateAddress: "10.73.156.12",
			},
		},
	}

	hosts, loadBalancer, err := vmFleetVars(fleet)
	require.NoError(t, err)
	require.Len(t, hosts, 2)
	require.Equal(t, "10.73.156.11", loadBalancer,
		"the nodes would join through an address the host owns rather than one they share")

	worker := hosts["kw0"]
	require.Equal(t, "127.0.0.1", worker.Address, "the suite reaches a VM on its forwarded port")
	require.Equal(t, 26242, worker.Port)
	require.Equal(t, key, worker.KeyPath)
	require.Equal(t, "worker", worker.Role)
	require.Equal(t, "10.73.156.12", worker.PrivateAddress)
	require.NotEmpty(t, worker.PrivateInterface, "without this the engine advertises the slirp address")
}

// TestVMFleetVarsEmpty is the case that would otherwise panic reading the leader out of an empty
// node list.
func TestVMFleetVarsEmpty(t *testing.T) {
	_, _, err := vmFleetVars(microvm.Fleet{Name: "tofu"})
	require.Error(t, err)
}
