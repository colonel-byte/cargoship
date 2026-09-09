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
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/stretchr/testify/require"
)

func TestNftRule(t *testing.T) {
	tests := []struct {
		name      string
		rule      cluster.ZarfFirewallRule
		wantChain string
		want      string
	}{
		{
			name: "in rule with a source and port",
			rule: cluster.ZarfFirewallRule{
				Name: "metrics", Action: "allow", Source: "10.0.0.0/8", Port: "9100", Protocol: "tcp",
			},
			wantChain: "input",
			want:      `ip saddr 10.0.0.0/8 tcp dport 9100 accept comment "cargoship:metrics"`,
		},
		{
			name:      "in rule defaults the direction",
			rule:      cluster.ZarfFirewallRule{Name: "peers", Action: "allow", Source: "10.0.0.5"},
			wantChain: "input",
			want:      `ip saddr 10.0.0.5 accept comment "cargoship:peers"`,
		},
		{
			name: "out rule",
			rule: cluster.ZarfFirewallRule{
				Name: "egress-dns", Action: "allow", Direction: "out",
				Destination: "10.43.0.10", Port: "53", Protocol: "udp",
			},
			wantChain: "output",
			want:      `ip daddr 10.43.0.10 udp dport 53 accept comment "cargoship:egress-dns"`,
		},
		{
			name: "forward rule uses interfaces",
			rule: cluster.ZarfFirewallRule{
				Name: "ingress-http", Action: "allow", Direction: "forward",
				Ingress: "eth0", Egress: "cni0", Port: "80", Protocol: "tcp",
			},
			wantChain: "forward",
			want:      `iifname "eth0" oifname "cni0" tcp dport 80 accept comment "cargoship:ingress-http"`,
		},
		{
			name: "port range keeps the neutral form",
			rule: cluster.ZarfFirewallRule{
				Name: "nodeports", Action: "allow", Port: "30000-32767", Protocol: "tcp",
			},
			wantChain: "input",
			want:      `tcp dport 30000-32767 accept comment "cargoship:nodeports"`,
		},
		{
			name: "an IPv6 source is matched in the ip6 family",
			rule: cluster.ZarfFirewallRule{
				Name: "v6-peers", Action: "allow", Source: "fd00::/8",
			},
			wantChain: "input",
			want:      `ip6 saddr fd00::/8 accept comment "cargoship:v6-peers"`,
		},
		{
			name:      "deny becomes drop",
			rule:      cluster.ZarfFirewallRule{Name: "block", Action: "deny", Source: "192.0.2.1"},
			wantChain: "input",
			want:      `ip saddr 192.0.2.1 drop comment "cargoship:block"`,
		},
		{
			name:      "reject stays reject",
			rule:      cluster.ZarfFirewallRule{Name: "refuse", Action: "reject", Source: "192.0.2.2"},
			wantChain: "input",
			want:      `ip saddr 192.0.2.2 reject comment "cargoship:refuse"`,
		},
		{
			name:      "a protocol with no port matches the protocol alone",
			rule:      cluster.ZarfFirewallRule{Name: "sctp-any", Action: "allow", Protocol: "sctp"},
			wantChain: "input",
			want:      `meta l4proto sctp accept comment "cargoship:sctp-any"`,
		},
		{
			name:      "a rule without a name is keyed by its fields",
			rule:      cluster.ZarfFirewallRule{Action: "allow", Source: "10.0.0.9"},
			wantChain: "input",
			want:      `ip saddr 10.0.0.9 accept comment "cargoship:in-allow-10-0-0-9"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain, got, err := nftRule(tt.rule)
			require.NoError(t, err)
			require.Equal(t, tt.wantChain, chain)
			require.Equal(t, tt.want, got)
		})
	}

	t.Run("an unknown action is an error", func(t *testing.T) {
		_, _, err := nftRule(cluster.ZarfFirewallRule{Name: "bad", Action: "log"})
		require.ErrorContains(t, err, "unknown action")
	})
}

func TestNftRuleset(t *testing.T) {
	plan := Plan{
		NodeAddresses: []string{"10.0.0.5", "10.0.0.6", "fd00::5"},
		ClusterCIDRs:  []string{"10.42.0.0/16", "10.43.0.0/16"},
		Ports: []cluster.ZarfHostPort{
			{Port: "6443", Protocol: "tcp"},
			{Port: "8472", Protocol: "udp"},
			{Port: "30000-32767"},
		},
		Rules: []cluster.ZarfFirewallRule{
			{Name: "metrics", Action: "allow", Source: "10.0.0.0/8", Port: "9100", Protocol: "tcp"},
			{Name: "ingress-http", Action: "allow", Direction: "forward", Ingress: "eth0", Egress: "cni0"},
			{Name: "egress-dns", Action: "allow", Direction: "out", Destination: "10.43.0.10", Port: "53", Protocol: "udp"},
		},
	}

	got, err := nftRuleset(plan)
	require.NoError(t, err)

	require.Equal(t, `#!/usr/sbin/nft -f
# Managed by cargoship. Edits are replaced on the next apply.
add table inet cargoship
delete table inet cargoship
table inet cargoship {
	set cluster_v4 {
		type ipv4_addr
		flags interval
		elements = { 10.0.0.5, 10.0.0.6, 10.42.0.0/16, 10.43.0.0/16 }
	}
	set cluster_v6 {
		type ipv6_addr
		flags interval
		elements = { fd00::5 }
	}
	set ports_tcp {
		type inet_service
		flags interval
		elements = { 6443, 30000-32767 }
	}
	set ports_udp {
		type inet_service
		flags interval
		elements = { 8472 }
	}
	chain input {
		type filter hook input priority filter; policy accept;
		ip saddr @cluster_v4 accept comment "cargoship:cluster"
		ip6 saddr @cluster_v6 accept comment "cargoship:cluster"
		tcp dport @ports_tcp accept comment "cargoship:ports"
		udp dport @ports_udp accept comment "cargoship:ports"
		ip saddr 10.0.0.0/8 tcp dport 9100 accept comment "cargoship:metrics"
	}
	chain forward {
		type filter hook forward priority filter; policy accept;
		iifname "eth0" oifname "cni0" accept comment "cargoship:ingress-http"
	}
	chain output {
		type filter hook output priority filter; policy accept;
		ip daddr 10.43.0.10 udp dport 53 accept comment "cargoship:egress-dns"
	}
}
`, got)
}

// TestNftRulesetTeardown covers the plan that has nothing in it, which is what a node gets
// when its inventory drops every rule. The table is still added before it is deleted, so the
// script also works on a node cargoship never configured.
func TestNftRulesetTeardown(t *testing.T) {
	got, err := nftRuleset(Plan{})
	require.NoError(t, err)

	require.Equal(t, `#!/usr/sbin/nft -f
# Managed by cargoship. Edits are replaced on the next apply.
add table inet cargoship
delete table inet cargoship
`, got)
}

// TestNftRulesetPolicies asserts every base chain keeps an accept policy, because a
// default-drop policy applied from a remote phase would cut cargoship's own connection.
func TestNftRulesetPolicies(t *testing.T) {
	got, err := nftRuleset(Plan{NodeAddresses: []string{"10.0.0.5"}})
	require.NoError(t, err)

	require.Equal(t, 3, strings.Count(got, "policy accept;"))
	require.NotContains(t, got, "policy drop")
	require.NotContains(t, got, "flush ruleset")
}

func TestNftRulesetError(t *testing.T) {
	_, err := nftRuleset(Plan{Rules: []cluster.ZarfFirewallRule{{Name: "bad", Action: "log"}}})
	require.ErrorContains(t, err, "unknown action")
}

func TestNftSplitFamilies(t *testing.T) {
	v4, v6 := nftSplitFamilies([]string{"10.0.0.5", "fd00::5", "10.42.0.0/16", "fd00::/8", "", "not-an-address"})

	require.Equal(t, []string{"10.0.0.5", "10.42.0.0/16"}, v4)
	require.Equal(t, []string{"fd00::5", "fd00::/8"}, v6)
}

func TestNftInclude(t *testing.T) {
	require.Equal(t, `include "/etc/cargoship/nftables.nft"`, nftInclude())
}
