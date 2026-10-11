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

	"github.com/stretchr/testify/require"
)

const testPublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyForTestsOnly cargoship-microvm"

// seedFor renders the three documents for one node of a two-node fleet.
func seedFor(t *testing.T, index int) map[string][]byte {
	t.Helper()

	nodes, err := Spec{
		Fleet:       "dev",
		Controllers: 1,
		Workers:     1,
	}.Nodes()
	require.NoError(t, err)

	files, err := renderSeedFiles(nodes[index], testPublicKey)
	require.NoError(t, err)
	return files
}

func TestRenderSeedFilesCoversEveryDocument(t *testing.T) {
	t.Parallel()

	files := seedFor(t, 0)
	for _, name := range seedFiles {
		require.Contains(t, files, name)
		require.NotEmpty(t, files[name], "%s rendered empty", name)
	}
	require.Len(t, files, len(seedFiles), "an extra document would be ignored by cloud-init")
}

func TestRenderSeedFilesUserData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want string
		why  string
	}{
		{
			name: "cloud-config header",
			want: "#cloud-config",
			why:  "cloud-init ignores a user-data document without it, silently",
		},
		{
			name: "the fleet key",
			want: testPublicKey,
			why:  "without it the node answers no key and the bring-up only times out",
		},
		{
			name: "root login is allowed",
			want: "name: root",
			why:  "the inventory connects as root",
		},
		{
			name: "fqdn differs from the hostname",
			want: "fqdn: kc0.cargoship.test",
			why:  "phase 25 writes LongHostname, which is only distinct with a domain",
		},
		{
			name: "firewalld is installed",
			want: "- firewalld",
			why:  "phase 26 detects by asking whether the service runs, and the image has none",
		},
		{
			name: "fapolicyd is installed",
			want: "- fapolicyd",
			why:  "phase 22 gates on the service running, and the image has none",
		},
		{
			name: "both services are started",
			want: "[systemctl, enable, --now, firewalld]",
			why:  "installing without starting leaves both gates shut",
		},
	}

	userData := string(seedFor(t, 0)["user-data"])
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Contains(t, userData, tt.want, tt.why)
		})
	}

	require.NotContains(t, userData, "SELINUX=",
		"the fleet asserts the image's enforcing default rather than setting it")
}

func TestRenderSeedFilesMetaData(t *testing.T) {
	t.Parallel()

	metaData := string(seedFor(t, 1)["meta-data"])
	require.Contains(t, metaData, "instance-id: kw0")
	require.Contains(t, metaData, "local-hostname: kw0")
}

// TestRenderSeedFilesNetworkConfig pins the part that is easiest to get subtly wrong: the
// private interface takes a static address and no DHCP, the management one takes DHCP and no
// address, and both are matched on MAC rather than by interface name.
func TestRenderSeedFilesNetworkConfig(t *testing.T) {
	t.Parallel()

	nodes, err := Spec{
		Fleet:       "dev",
		Controllers: 1,
		Workers:     1,
	}.Nodes()
	require.NoError(t, err)

	for _, n := range nodes {
		files, err := renderSeedFiles(n, testPublicKey)
		require.NoError(t, err)
		got := stripComments(string(files["network-config"]))

		require.Contains(t, got, `macaddress: "`+n.MgmtMAC+`"`)
		require.Contains(t, got, `macaddress: "`+n.LANMAC+`"`)
		require.Contains(t, got, "- "+n.PrivateAddress+"/24")
		require.NotContains(t, got, "enp0s",
			"matching by interface name would break when a PCI device is added ahead of the NICs")
		require.NotContains(t, got, "gateway",
			"the default route belongs on the management interface, not the private segment")
	}
}

// stripComments drops comment lines so that an assertion about what the document declares is
// not satisfied, or broken, by prose explaining it.
func stripComments(doc string) string {
	var kept []string
	for line := range strings.SplitSeq(doc, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
