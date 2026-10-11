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
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// argvFor assembles the command line for one node of a fleet.
func argvFor(t *testing.T, spec Spec, index int) (Node, []string) {
	t.Helper()

	normalized, err := spec.Normalize()
	require.NoError(t, err)
	nodes, err := normalized.Nodes()
	require.NoError(t, err)

	return nodes[index], qemuArgs(normalized, nodes[index], "/run/fleet/"+nodes[index].Name)
}

func TestQemuArgsFlags(t *testing.T) {
	t.Parallel()

	_, argv := argvFor(t, Spec{
		Fleet:       "dev",
		Controllers: 1,
		Workers:     1,
		MemoryMiB:   2048,
		CPUs:        4,
	}, 0)
	joined := strings.Join(argv, " ")

	tests := []struct {
		name string
		want string
		why  string
	}{
		{
			name: "kvm acceleration",
			want: "q35,accel=kvm",
			why:  "emulation is too slow to run an engine in",
		},
		{
			name: "the requested cpu count",
			want: "-smp 4",
			why:  "",
		},
		{
			name: "the requested memory",
			want: "-m 2048",
			why:  "",
		},
		{
			name: "display none, not nographic",
			want: "-display none",
			why:  "qemu refuses -nographic with -daemonize, and only says so once launched",
		},
		{
			name: "detached",
			want: "-daemonize",
			why:  "the fleet outlives the command that started it",
		},
		{
			name: "a pidfile",
			want: "-pidfile /run/fleet/kc0/pid",
			why:  "it is the only record of the process, and what Down signals",
		},
		{
			name: "the console on a file",
			want: "-serial file:/run/fleet/kc0/console.log",
			why:  "a node that never reaches sshd explains itself only there",
		},
		{
			name: "a disposable overlay",
			want: "cache=unsafe",
			why:  "there is no state on these disks worth an fsync",
		},
		{
			name: "the seed is read-only",
			want: "format=raw,readonly=on",
			why:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Contains(t, joined, tt.want, tt.why)
		})
	}

	require.NotContains(t, joined, "-nographic",
		"it is mutually exclusive with -daemonize")
}

// TestQemuArgsNetworking is the part that earns its test: two interfaces, each on the MAC the
// seed matches, with the private one on a multicast socket so that the fleet shares a
// broadcast domain without a tap device or root.
func TestQemuArgsNetworking(t *testing.T) {
	t.Parallel()

	spec := Spec{
		Fleet:       "dev",
		Controllers: 1,
		Workers:     1,
	}
	node, argv := argvFor(t, spec, 1)
	joined := strings.Join(argv, " ")

	require.Contains(t, joined, "hostfwd=tcp:127.0.0.1:"+strconv.Itoa(node.SSHPort)+"-:22")
	require.Contains(t, joined, "virtio-net-pci,netdev=mgmt,mac="+node.MgmtMAC)
	require.Contains(t, joined, "virtio-net-pci,netdev=lan,mac="+node.LANMAC)
	require.Contains(t, joined, "socket,id=lan,mcast="+mcastFor(fleetID("dev"))+",localaddr=127.0.0.1")

	require.Contains(t, joined, "127.0.0.1:",
		"every forward binds loopback; a fleet is not something to expose on a LAN")
	require.NotContains(t, joined, "netdev=tap",
		"a tap device would need root and would be subject to the host firewall")
}

// TestQemuArgsLeaderForwardsEnginePorts covers the forwards that make kube-config work from
// the management node rather than only from inside the fleet.
func TestQemuArgsLeaderForwardsEnginePorts(t *testing.T) {
	t.Parallel()

	leader, argv := argvFor(t, Spec{
		Fleet:       "dev",
		Controllers: 1,
		Workers:     1,
		Distro:      "rke2",
	}, 0)
	joined := strings.Join(argv, " ")

	require.Len(t, leader.HostFwd, 2)
	for _, f := range leader.HostFwd {
		require.Contains(t, joined, "hostfwd=tcp:127.0.0.1:"+strconv.Itoa(f.HostPort)+"-:"+strconv.Itoa(f.GuestPort))
	}

	_, workerArgv := argvFor(t, Spec{
		Fleet:       "dev",
		Controllers: 1,
		Workers:     1,
		Distro:      "rke2",
	}, 1)
	require.NotContains(t, strings.Join(workerArgv, " "), "-:6443",
		"only the leader forwards the api port")
}
