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

package firewall

import (
	"testing"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/stretchr/testify/require"
)

func TestFirewalldZoneCmd(t *testing.T) {
	tests := []struct {
		name string
		rule cluster.ZarfFirewallRule
		want string
	}{
		{
			name: "port only becomes add-port",
			rule: cluster.ZarfFirewallRule{Action: "allow", Port: "6443", Protocol: "tcp"},
			want: "firewall-cmd --permanent --zone=public --add-port=6443/tcp",
		},
		{
			name: "port range keeps firewalld's dash form",
			rule: cluster.ZarfFirewallRule{Action: "allow", Port: "30000-32767", Protocol: "tcp"},
			want: "firewall-cmd --permanent --zone=public --add-port=30000-32767/tcp",
		},
		{
			name: "a source becomes a rich rule",
			rule: cluster.ZarfFirewallRule{Action: "allow", Source: "10.0.0.0/8", Port: "9100", Protocol: "tcp"},
			want: `firewall-cmd --permanent --zone=public --add-rich-rule='rule family="ipv4" source address="10.0.0.0/8" port port="9100" protocol="tcp" accept'`,
		},
		{
			name: "protocol without a port",
			rule: cluster.ZarfFirewallRule{Action: "allow", Source: "10.0.0.0/8", Protocol: "sctp"},
			want: `firewall-cmd --permanent --zone=public --add-rich-rule='rule family="ipv4" source address="10.0.0.0/8" protocol value="sctp" accept'`,
		},
		{
			name: "an ipv6 address picks the ipv6 family",
			rule: cluster.ZarfFirewallRule{Action: "deny", Source: "fd00::/8"},
			want: `firewall-cmd --permanent --zone=public --add-rich-rule='rule family="ipv6" source address="fd00::/8" drop'`,
		},
		{
			name: "an out rule matches on the destination",
			rule: cluster.ZarfFirewallRule{
				Action: "reject", Direction: "out", Destination: "10.43.0.10", Port: "53", Protocol: "udp",
			},
			want: `firewall-cmd --permanent --zone=public --add-rich-rule='rule family="ipv4" destination address="10.43.0.10" port port="53" protocol="udp" reject'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := firewalldZoneCmd(tt.rule)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}

	t.Run("no match at all", func(t *testing.T) {
		_, err := firewalldZoneCmd(cluster.ZarfFirewallRule{Action: "allow"})
		require.ErrorContains(t, err, "needs a source, destination, or port")
	})

	t.Run("a bare port cannot be denied", func(t *testing.T) {
		_, err := firewalldZoneCmd(cluster.ZarfFirewallRule{Action: "deny", Port: "80", Protocol: "tcp"})
		require.ErrorContains(t, err, "can only allow a port")
	})

	t.Run("unknown action", func(t *testing.T) {
		_, err := firewalldZoneCmd(cluster.ZarfFirewallRule{Action: "drop", Port: "80", Protocol: "tcp"})
		require.ErrorContains(t, err, "unknown firewall action")
	})
}

func TestFirewalldPolicy(t *testing.T) {
	got, err := firewalldPolicy(cluster.ZarfFirewallRule{
		Name: "ingress-http", Action: "allow", Direction: "forward",
		Ingress: "public", Egress: "trusted", Port: "80", Protocol: "tcp",
	})
	require.NoError(t, err)
	require.Equal(t, "ACCEPT", got.Target)
	require.Equal(t, "public", got.Ingress.Name)
	require.Equal(t, "trusted", got.Egress.Name)
	require.Equal(t, []cluster.ZarfFirewallPort{{Port: "80", Protocol: "tcp"}}, got.Ports)
	require.Equal(t, firewalldPolicyShort, got.Short)

	t.Run("no port means no port element", func(t *testing.T) {
		got, err := firewalldPolicy(cluster.ZarfFirewallRule{
			Action: "reject", Direction: "forward", Ingress: "public", Egress: "trusted",
		})
		require.NoError(t, err)
		require.Equal(t, "REJECT", got.Target)
		require.Empty(t, got.Ports)
	})
}

func TestFirewalldTarget(t *testing.T) {
	for action, want := range map[string]string{"allow": "ACCEPT", "deny": "DROP", "reject": "REJECT"} {
		got, err := firewalldTarget(action)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}

	_, err := firewalldTarget("continue")
	require.Error(t, err)
}

func TestPlanIsEmpty(t *testing.T) {
	require.True(t, Plan{}.IsEmpty())
	require.False(t, Plan{NodeAddresses: []string{"10.0.0.1"}}.IsEmpty())
	require.False(t, Plan{Ports: []cluster.ZarfHostPort{{Port: "80", Protocol: "tcp"}}}.IsEmpty())
	require.False(t, Plan{Policies: map[string]cluster.ZarfFirewallPolicyConfig{"p": {}}}.IsEmpty())
}

func TestSortedKeys(t *testing.T) {
	got := sortedKeys(map[string]int{"c": 1, "a": 2, "b": 3})
	require.Equal(t, []string{"a", "b", "c"}, got)
}

func TestBackendsOrder(t *testing.T) {
	got := Backends()
	require.Len(t, got, 2)
	require.Equal(t, FirewalldService, got[0].Name())
	require.Equal(t, UFWService, got[1].Name())
}

func TestDetectWithoutAConfigurer(t *testing.T) {
	require.False(t, (&Firewalld{}).Detect(&cluster.ZarfHost{}))
	require.False(t, (&UFW{}).Detect(&cluster.ZarfHost{}))
	require.Nil(t, For(&cluster.ZarfHost{}))
}
