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
	"os"
	"path/filepath"
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

// TestParseCloudInitStatus covers the gate that decides a node is usable. Treating "running"
// as done is the bug this replaced: sshd answers about six seconds in, and the packages that
// open the fapolicyd and firewall gates are installed well after that, so a fleet handed back
// early looks healthy with both of those phases silently skipping.
func TestParseCloudInitStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		out  string
		want string
	}{
		{
			name: "finished",
			out:  "status: done\nboot_status_code: enabled-by-generator\n",
			want: "done",
		},
		{
			name: "still working",
			out:  "status: running\nextended_status: running\ndetail: DataSourceNoCloud [seed=/dev/vdb]\n",
			want: "running",
		},
		{
			name: "failed",
			out:  "status: error\nerrors:\n  - something went wrong\n",
			want: "error",
		},
		{
			name: "never started",
			out:  "status: disabled\n",
			want: "disabled",
		},
		{
			name: "no status line reads as still working",
			out:  "boot_status_code: enabled-by-generator\n",
			want: "running",
		},
		{
			name: "nothing at all reads as still working",
			out:  "",
			want: "running",
		},
		{
			name: "indented, as the long form prints it",
			out:  "  status: done  \n",
			want: "done",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, parseCloudInitStatus(tt.out))
		})
	}
}

// TestLoadCountsEachGroup covers the reconstruction every command but Up depends on: nothing
// per-node is stored, so Down, List and SSH rebuild the fleet from the directory names alone.
// Miscounting a group there gives every node after it the wrong role, port and address.
func TestLoadCountsEachGroup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		dirs    []string
		want    []string
		wantErr string
	}{
		{
			name: "controllers and workers",
			dirs: []string{"kc0", "kw0", "kw1"},
			want: []string{"kc0", "kw0", "kw1"},
		},
		{
			name: "infra is not counted as a worker",
			dirs: []string{"kc0", "kw0", "ki0", "ki1"},
			want: []string{"kc0", "kw0", "ki0", "ki1"},
		},
		{
			name: "infra only",
			dirs: []string{"kc0", "ki0"},
			want: []string{"kc0", "ki0"},
		},
		{
			name:    "no controller directory",
			dirs:    []string{"kw0", "ki0"},
			wantErr: "no controller directory",
		},
		{
			name:    "nothing there at all",
			dirs:    nil,
			wantErr: "no fleet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			spec, err := Spec{
				Fleet:       "dev",
				Controllers: 1,
				RunRoot:     root,
			}.Normalize()
			require.NoError(t, err)

			for _, d := range tt.dirs {
				require.NoError(t, os.MkdirAll(filepath.Join(spec.dir(), d), 0o755))
			}
			// A stray file must not be counted as a node.
			if tt.dirs != nil {
				require.NoError(t, os.WriteFile(filepath.Join(spec.dir(), "inventory.yaml"), []byte("{}"), 0o600))
			}

			fleet, err := load(spec)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)

			var got []string
			for _, n := range fleet.Nodes {
				got = append(got, n.Name)
			}
			require.Equal(t, tt.want, got)
			require.True(t, filepath.IsAbs(fleet.KeyPath),
				"the key path is written into the inventory, which cargoship reads from elsewhere")
		})
	}
}
