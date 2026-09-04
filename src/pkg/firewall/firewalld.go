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
	"context"
	"encoding/xml"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/k0sproject/rig/exec"
)

const (
	// FirewalldService is the name of the firewalld system service.
	FirewalldService = "firewalld"
	// firewalldNodeIPSet is the ipset holding every node's private address.
	firewalldNodeIPSet = "k8s-nodes"
	// firewalldClusterIPSet is the ipset holding the pod and service CIDRs.
	firewalldClusterIPSet = "k8s-subnets"
	// firewalldPortService is the firewalld service cargoship writes the inventory ports into.
	firewalldPortService = "distro-exposed-ports"
	// firewalldPolicyShort is the description cargoship stamps on the policies it writes.
	firewalldPolicyShort = "Cargoship Policy"
	// firewalldRulePrefix names the policy files cargoship writes for neutral forward rules,
	// keeping them apart from the legacy `.host.policy` files.
	firewalldRulePrefix = "cargoship-"
)

// selfClosing rewrites an empty XML element pair into a self-closing tag, which is the form
// firewalld writes itself.
var selfClosing = regexp.MustCompile(`></.+>`)

// ipSetConfig is a firewalld ipset holding a set of addresses or CIDRs.
type ipSetConfig struct {
	XMLName xml.Name `xml:"ipset"`
	Type    string   `xml:"type,attr"`
	Short   string   `xml:"short"`
	Long    string   `xml:"description"`
	Entries []string `xml:"entry"`
}

// portServiceConfig is a firewalld service definition holding a set of ports.
type portServiceConfig struct {
	XMLName xml.Name               `xml:"service"`
	Short   string                 `xml:"short"`
	Ports   []cluster.ZarfHostPort `xml:"port"`
}

// Firewalld applies a Plan to a node running firewalld.
type Firewalld struct{}

var _ Backend = (*Firewalld)(nil)

// Name is the backend identifier.
func (f *Firewalld) Name() string {
	return FirewalldService
}

// Detect is true when firewalld is running on h.
func (f *Firewalld) Detect(h *cluster.ZarfHost) bool {
	if h == nil || h.Configurer == nil {
		return false
	}

	return h.Configurer.ServiceIsRunning(h, FirewalldService)
}

// Apply writes the ipsets, service, and policy files for p, enables them, and restarts
// firewalld.
func (f *Firewalld) Apply(ctx context.Context, h *cluster.ZarfHost, p Plan) error {
	if err := f.applyClusterTrust(h, p); err != nil {
		return err
	}
	if err := f.applyPorts(h, p); err != nil {
		return err
	}
	if err := f.applyPolicies(h, p); err != nil {
		return err
	}
	if err := f.applyRules(ctx, h, p); err != nil {
		return err
	}

	return h.Configurer.RestartService(h, FirewalldService)
}

// applyClusterTrust trusts every node address and cluster CIDR.
func (f *Firewalld) applyClusterTrust(h *cluster.ZarfHost, p Plan) error {
	sets := []struct {
		name    string
		kind    string
		short   string
		long    string
		entries []string
	}{
		{firewalldNodeIPSet, "hash:ip", "k8nodes", "IPset for all k8 nodes", p.NodeAddresses},
		{firewalldClusterIPSet, "hash:net", "k8subnets", "IPset for all k8 service and pod CIDRs", p.ClusterCIDRs},
	}

	for _, set := range sets {
		if len(set.entries) == 0 {
			continue
		}

		output, err := xml.MarshalIndent(ipSetConfig{
			Type:    set.kind,
			Short:   set.short,
			Long:    set.long,
			Entries: set.entries,
		}, "", "  ")
		if err != nil {
			return err
		}

		path := fmt.Sprintf("/etc/firewalld/ipsets/%s.xml", set.name)
		if err := h.WriteFile(path, string(output)+"\n", "0600"); err != nil {
			return err
		}

		cmd := fmt.Sprintf("firewall-cmd --permanent --zone=trusted --add-source=ipset:%s", set.name)
		if err := h.Exec(cmd, exec.Sudo(h)); err != nil {
			return err
		}
	}

	return nil
}

// applyPorts opens the inventory ports on the public zone.
func (f *Firewalld) applyPorts(h *cluster.ZarfHost, p Plan) error {
	if len(p.Ports) == 0 {
		return nil
	}

	output, err := xml.MarshalIndent(portServiceConfig{
		Short: firewalldPortService,
		Ports: p.Ports,
	}, "", "  ")
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/etc/firewalld/services/%s.xml", firewalldPortService)
	if err := h.WriteFile(path, string(output)+"\n", "0600"); err != nil {
		return err
	}

	cmd := fmt.Sprintf("firewall-cmd --permanent --zone=public --add-service=%s", firewalldPortService)

	return h.Exec(cmd, exec.Sudo(h))
}

// applyPolicies writes the legacy `.host.policy` files.
func (f *Firewalld) applyPolicies(h *cluster.ZarfHost, p Plan) error {
	for _, name := range sortedKeys(p.Policies) {
		policy := p.Policies[name]
		policy.Short = firewalldPolicyShort
		if err := writePolicy(h, name, policy); err != nil {
			return err
		}
	}

	return nil
}

// applyRules renders the backend-neutral rules. Forward rules become firewalld policies;
// in and out rules become ports or rich rules on the public zone.
func (f *Firewalld) applyRules(_ context.Context, h *cluster.ZarfHost, p Plan) error {
	for _, rule := range p.Rules {
		if rule.NormalizedDirection() == cluster.FirewallDirectionForward {
			policy, err := firewalldPolicy(rule)
			if err != nil {
				return err
			}
			if err := writePolicy(h, firewalldRulePrefix+rule.Key(), policy); err != nil {
				return err
			}

			continue
		}

		cmd, err := firewalldZoneCmd(rule)
		if err != nil {
			return err
		}
		if err := h.Exec(cmd, exec.Sudo(h)); err != nil {
			return err
		}
	}

	return nil
}

// writePolicy marshals a policy to firewalld's XML and writes it under /etc/firewalld/policies.
func writePolicy(h *cluster.ZarfHost, name string, policy cluster.ZarfFirewallPolicyConfig) error {
	output, err := xml.MarshalIndent(policy, "", "  ")
	if err != nil {
		return err
	}

	out := selfClosing.ReplaceAllString(string(output), "/>")

	return h.WriteFile(fmt.Sprintf("/etc/firewalld/policies/%s.xml", name), out+"\n", "0600")
}

// firewalldPolicy converts a neutral forward rule into a firewalld policy.
func firewalldPolicy(rule cluster.ZarfFirewallRule) (cluster.ZarfFirewallPolicyConfig, error) {
	target, err := firewalldTarget(rule.NormalizedAction())
	if err != nil {
		return cluster.ZarfFirewallPolicyConfig{}, err
	}

	policy := cluster.ZarfFirewallPolicyConfig{
		Short:   firewalldPolicyShort,
		Target:  target,
		Ingress: cluster.ZarfFirewallZone{Name: rule.Ingress},
		Egress:  cluster.ZarfFirewallZone{Name: rule.Egress},
	}
	if rule.Port != "" {
		policy.Ports = []cluster.ZarfFirewallPort{{
			Port:     rule.Port,
			Protocol: rule.NormalizedProtocol(),
		}}
	}

	return policy, nil
}

// firewalldZoneCmd converts a neutral in or out rule into a firewall-cmd invocation on the
// public zone. A rule that only names a port becomes an --add-port; anything that matches on
// an address becomes a rich rule.
func firewalldZoneCmd(rule cluster.ZarfFirewallRule) (string, error) {
	target, err := firewalldTarget(rule.NormalizedAction())
	if err != nil {
		return "", err
	}

	if rule.Source == "" && rule.Destination == "" && rule.NormalizedDirection() == cluster.FirewallDirectionIn {
		if rule.Port == "" {
			return "", fmt.Errorf("firewall rule %q: firewalld needs a source, destination, or port", rule.Key())
		}
		if target != "ACCEPT" {
			return "", fmt.Errorf("firewall rule %q: firewalld can only allow a port with no address match", rule.Key())
		}

		return fmt.Sprintf(
			"firewall-cmd --permanent --zone=public --add-port=%s/%s",
			rule.Port, rule.NormalizedProtocol(),
		), nil
	}

	parts := []string{fmt.Sprintf("rule family=%q", firewalldFamily(rule))}
	if rule.Source != "" {
		parts = append(parts, fmt.Sprintf("source address=%q", rule.Source))
	}
	if rule.Destination != "" {
		parts = append(parts, fmt.Sprintf("destination address=%q", rule.Destination))
	}
	if rule.Port != "" {
		parts = append(parts, fmt.Sprintf("port port=%q protocol=%q", rule.Port, rule.NormalizedProtocol()))
	} else if proto := rule.NormalizedProtocol(); proto != "" {
		parts = append(parts, fmt.Sprintf("protocol value=%q", proto))
	}
	parts = append(parts, strings.ToLower(target))

	return fmt.Sprintf(
		"firewall-cmd --permanent --zone=public --add-rich-rule='%s'",
		strings.Join(parts, " "),
	), nil
}

// firewalldTarget maps a neutral action onto a firewalld target.
func firewalldTarget(action string) (string, error) {
	switch action {
	case cluster.FirewallActionAllow:
		return "ACCEPT", nil
	case cluster.FirewallActionDeny:
		return "DROP", nil
	case cluster.FirewallActionReject:
		return "REJECT", nil
	default:
		return "", fmt.Errorf("unknown firewall action %q", action)
	}
}

// firewalldFamily picks the rich rule address family for a rule.
func firewalldFamily(rule cluster.ZarfFirewallRule) string {
	if strings.Contains(rule.Source, ":") || strings.Contains(rule.Destination, ":") {
		return "ipv6"
	}

	return "ipv4"
}

// sortedKeys returns a map's keys in a stable order, so cargoship writes the same files in
// the same order on every run.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return keys
}
