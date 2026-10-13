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

func TestFirewallRuleValidate(t *testing.T) {
	tests := []struct {
		name    string
		rule    ZarfFirewallRule
		wantErr string
	}{
		{
			name: "minimal in rule",
			rule: ZarfFirewallRule{Action: "allow", Source: "10.0.0.0/8"},
		},
		{
			name: "port and protocol",
			rule: ZarfFirewallRule{Action: "allow", Port: "6443", Protocol: "tcp"},
		},
		{
			name: "port range",
			rule: ZarfFirewallRule{Action: "allow", Port: "30000-32767", Protocol: "udp"},
		},
		{
			name: "forward rule",
			rule: ZarfFirewallRule{Action: "allow", Direction: "forward", Ingress: "public", Egress: "trusted"},
		},
		{
			name:    "missing action",
			rule:    ZarfFirewallRule{Source: "10.0.0.0/8"},
			wantErr: "action is required",
		},
		{
			name:    "unknown action",
			rule:    ZarfFirewallRule{Action: "drop"},
			wantErr: "unknown action",
		},
		{
			name:    "unknown direction",
			rule:    ZarfFirewallRule{Action: "allow", Direction: "sideways"},
			wantErr: "unknown direction",
		},
		{
			name:    "forward without zones",
			rule:    ZarfFirewallRule{Action: "allow", Direction: "forward", Ingress: "public"},
			wantErr: "need both ingress and egress",
		},
		{
			name:    "zones on an in rule",
			rule:    ZarfFirewallRule{Action: "allow", Ingress: "public"},
			wantErr: "apply to forward rules only",
		},
		{
			name:    "unknown protocol",
			rule:    ZarfFirewallRule{Action: "allow", Port: "80", Protocol: "icmp"},
			wantErr: "unknown protocol",
		},
		{
			name:    "port without protocol",
			rule:    ZarfFirewallRule{Action: "allow", Port: "80"},
			wantErr: "needs a protocol",
		},
		{
			name:    "port out of range",
			rule:    ZarfFirewallRule{Action: "allow", Port: "70000", Protocol: "tcp"},
			wantErr: "out of range",
		},
		{
			name:    "descending range",
			rule:    ZarfFirewallRule{Action: "allow", Port: "500-100", Protocol: "tcp"},
			wantErr: "does not ascend",
		},
		{
			name:    "bad cidr",
			rule:    ZarfFirewallRule{Action: "allow", Source: "10.0.0.0/64"},
			wantErr: "invalid source CIDR",
		},
		{
			name:    "bad address",
			rule:    ZarfFirewallRule{Action: "allow", Destination: "not-an-ip"},
			wantErr: "invalid destination address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rule.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestFirewallConfigValidate(t *testing.T) {
	t.Run("duplicate names", func(t *testing.T) {
		cfg := ZarfFirewallConfig{Rules: []ZarfFirewallRule{
			{Name: "metrics", Action: "allow", Port: "9100", Protocol: "tcp"},
			{Name: "metrics", Action: "allow", Port: "9101", Protocol: "tcp"},
		}}
		require.ErrorContains(t, cfg.Validate(), "name is not unique")
	})

	t.Run("empty config", func(t *testing.T) {
		require.NoError(t, ZarfFirewallConfig{}.Validate())
	})
}

func TestFirewallRuleKey(t *testing.T) {
	t.Run("sanitizes a given name", func(t *testing.T) {
		rule := ZarfFirewallRule{Name: "Allow Metrics/9100", Action: "allow"}
		require.Equal(t, "allow-metrics-9100", rule.Key())
	})

	t.Run("derives a name from the match fields", func(t *testing.T) {
		rule := ZarfFirewallRule{Action: "allow", Source: "10.0.0.0/8", Port: "9345", Protocol: "TCP"}
		require.Equal(t, "in-allow-10-0-0-0-8-9345-tcp", rule.Key())
	})

	t.Run("differs when the match fields differ", func(t *testing.T) {
		first := ZarfFirewallRule{Action: "allow", Source: "10.0.0.0/8"}
		second := ZarfFirewallRule{Action: "allow", Source: "10.1.0.0/16"}
		require.NotEqual(t, first.Key(), second.Key())
	})
}

func TestHostConfigMergeFirewall(t *testing.T) {
	t.Run("takes rules from the profile when the host has none", func(t *testing.T) {
		c := ZarfHostConfig{}
		c.Merge(ZarfHostConfig{Firewall: ZarfFirewallConfig{Rules: []ZarfFirewallRule{
			{Name: "api", Action: "allow", Port: "6443", Protocol: "tcp"},
		}}})
		require.Len(t, c.Firewall.Rules, 1)
		require.Equal(t, "api", c.Firewall.Rules[0].Name)
	})

	t.Run("unions the host rules with the profile rules", func(t *testing.T) {
		c := ZarfHostConfig{Firewall: ZarfFirewallConfig{Rules: []ZarfFirewallRule{
			{Name: "http", Action: "allow", Port: "80", Protocol: "tcp"},
		}}}
		c.Merge(ZarfHostConfig{Firewall: ZarfFirewallConfig{Rules: []ZarfFirewallRule{
			{Name: "api", Action: "allow", Port: "6443", Protocol: "tcp"},
		}}})
		require.Len(t, c.Firewall.Rules, 2)
		require.Equal(t, "http", c.Firewall.Rules[0].Name)
		require.Equal(t, "api", c.Firewall.Rules[1].Name)
	})

	t.Run("a host rule of the same name wins", func(t *testing.T) {
		c := ZarfHostConfig{Firewall: ZarfFirewallConfig{Rules: []ZarfFirewallRule{
			{Name: "api", Action: "allow", Port: "6443", Protocol: "tcp"},
		}}}
		c.Merge(ZarfHostConfig{Firewall: ZarfFirewallConfig{Rules: []ZarfFirewallRule{
			{Name: "api", Action: "deny", Port: "6443", Protocol: "tcp"},
		}}})
		require.Len(t, c.Firewall.Rules, 1)
		require.Equal(t, "allow", c.Firewall.Rules[0].Action)
	})

	t.Run("an unnamed duplicate is not added twice", func(t *testing.T) {
		rule := ZarfFirewallRule{Action: "allow", Source: "10.0.0.0/8", Port: "9100", Protocol: "tcp"}
		c := ZarfHostConfig{Firewall: ZarfFirewallConfig{Rules: []ZarfFirewallRule{rule}}}
		c.Merge(ZarfHostConfig{Firewall: ZarfFirewallConfig{Rules: []ZarfFirewallRule{rule}}})
		require.Len(t, c.Firewall.Rules, 1)
	})

	t.Run("merging an empty profile leaves the host alone", func(t *testing.T) {
		c := ZarfHostConfig{Firewall: ZarfFirewallConfig{Rules: []ZarfFirewallRule{
			{Name: "http", Action: "allow", Port: "80", Protocol: "tcp"},
		}}}
		c.Merge(ZarfHostConfig{})
		require.Len(t, c.Firewall.Rules, 1)
	})
}
