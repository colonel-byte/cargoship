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

func TestUFWRule(t *testing.T) {
	tests := []struct {
		name string
		rule cluster.ZarfFirewallRule
		want string
	}{
		{
			name: "in rule with a source and port",
			rule: cluster.ZarfFirewallRule{
				Name: "metrics", Action: "allow", Source: "10.0.0.0/8", Port: "9100", Protocol: "tcp",
			},
			want: "allow in from 10.0.0.0/8 to any port 9100 proto tcp comment 'cargoship:metrics'",
		},
		{
			name: "in rule defaults the direction",
			rule: cluster.ZarfFirewallRule{Name: "peers", Action: "allow", Source: "10.0.0.5"},
			want: "allow in from 10.0.0.5 to any comment 'cargoship:peers'",
		},
		{
			name: "out rule",
			rule: cluster.ZarfFirewallRule{
				Name: "egress-dns", Action: "allow", Direction: "out",
				Destination: "10.43.0.10", Port: "53", Protocol: "udp",
			},
			want: "allow out from any to 10.43.0.10 port 53 proto udp comment 'cargoship:egress-dns'",
		},
		{
			name: "forward rule uses interfaces",
			rule: cluster.ZarfFirewallRule{
				Name: "ingress-http", Action: "allow", Direction: "forward",
				Ingress: "eth0", Egress: "cni0", Port: "80", Protocol: "tcp",
			},
			want: "route allow in on eth0 out on cni0 from any to any port 80 proto tcp comment 'cargoship:ingress-http'",
		},
		{
			name: "port range becomes ufw's colon form",
			rule: cluster.ZarfFirewallRule{
				Name: "nodeports", Action: "allow", Port: "30000-32767", Protocol: "tcp",
			},
			want: "allow in from any to any port 30000:32767 proto tcp comment 'cargoship:nodeports'",
		},
		{
			name: "deny and reject pass through",
			rule: cluster.ZarfFirewallRule{Name: "block", Action: "reject", Source: "192.168.1.0/24"},
			want: "reject in from 192.168.1.0/24 to any comment 'cargoship:block'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ufwRule(tt.rule)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}

	t.Run("unknown action", func(t *testing.T) {
		_, err := ufwRule(cluster.ZarfFirewallRule{Action: "drop"})
		require.ErrorContains(t, err, "unknown action")
	})
}

func TestUFWRules(t *testing.T) {
	plan := Plan{
		NodeAddresses: []string{"10.0.0.1", "10.0.0.2"},
		ClusterCIDRs:  []string{"10.42.0.0/16"},
		Ports:         []cluster.ZarfHostPort{{Port: "6443", Protocol: "tcp"}},
		Rules: []cluster.ZarfFirewallRule{
			{Name: "metrics", Action: "allow", Source: "10.0.0.0/8", Port: "9100", Protocol: "tcp"},
		},
	}

	got, err := ufwRules(plan)
	require.NoError(t, err)
	require.Equal(t, []string{
		"allow cargoship-ports comment 'cargoship:ports'",
		"allow from 10.0.0.1 comment 'cargoship:cluster'",
		"allow from 10.0.0.2 comment 'cargoship:cluster'",
		"allow from 10.42.0.0/16 comment 'cargoship:cluster'",
		"allow in from 10.0.0.0/8 to any port 9100 proto tcp comment 'cargoship:metrics'",
	}, got)

	t.Run("no ports means no profile rule", func(t *testing.T) {
		got, err := ufwRules(Plan{NodeAddresses: []string{"10.0.0.1"}})
		require.NoError(t, err)
		require.Equal(t, []string{"allow from 10.0.0.1 comment 'cargoship:cluster'"}, got)
	})

	t.Run("the plan is left alone", func(t *testing.T) {
		_, err := ufwRules(plan)
		require.NoError(t, err)
		require.Equal(t, []string{"10.0.0.1", "10.0.0.2"}, plan.NodeAddresses)
	})
}

func TestUFWProfile(t *testing.T) {
	got := ufwProfile([]cluster.ZarfHostPort{
		{Port: "6443", Protocol: "tcp"},
		{Port: "30000-32767", Protocol: "udp"},
		{Port: "9345"},
	})

	require.Equal(t, `[cargoship-ports]
title=Cargoship exposed ports
description=Ports cargoship opens on this node for the cluster
ports=6443/tcp|30000:32767/udp|9345/tcp
`, got)
}

func TestStripComment(t *testing.T) {
	require.Equal(t,
		"allow in from 10.0.0.0/8 to any",
		stripComment("allow in from 10.0.0.0/8 to any comment 'cargoship:metrics'"),
	)
	require.Equal(t, "allow from 10.0.0.1", stripComment("allow from 10.0.0.1"))
}
