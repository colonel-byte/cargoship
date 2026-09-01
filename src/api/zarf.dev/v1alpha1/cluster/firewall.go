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
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

const (
	// FirewallActionAllow lets matching traffic through.
	FirewallActionAllow = "allow"
	// FirewallActionDeny drops matching traffic without a reply.
	FirewallActionDeny = "deny"
	// FirewallActionReject refuses matching traffic with an ICMP error.
	FirewallActionReject = "reject"

	// FirewallDirectionIn matches traffic arriving at the node.
	FirewallDirectionIn = "in"
	// FirewallDirectionOut matches traffic leaving the node.
	FirewallDirectionOut = "out"
	// FirewallDirectionForward matches traffic routed through the node, from one
	// zone or interface to another.
	FirewallDirectionForward = "forward"
)

// portPattern matches a single port ("6443") or an inclusive range ("30000-32767").
var portPattern = regexp.MustCompile(`^\d{1,5}(-\d{1,5})?$`)

// ZarfFirewallConfig is the backend-neutral firewall configuration for a node. Cargoship
// renders these rules onto whichever firewall backend the node runs, so a single inventory
// can target both firewalld and ufw hosts.
type ZarfFirewallConfig struct {
	// Rules lists the firewall rules cargoship applies to the node.
	Rules []ZarfFirewallRule `json:"rules,omitempty"`
}

// ZarfFirewallRule is a single backend-neutral firewall rule. Every field except Action is
// optional; an omitted match field means "any". Backends translate a rule into their own
// dialect, so not every combination is expressible everywhere -- see the firewall package.
type ZarfFirewallRule struct {
	// Name identifies the rule. Cargoship uses it to name the artifacts it writes on the
	// node, so it must be unique within a host. Cargoship generates one when it is empty.
	Name string `json:"name,omitempty" jsonschema:"example=allow-metrics"`
	// Action is what cargoship does with traffic that matches the rule.
	Action string `json:"action" jsonschema:"required,enum=allow,enum=deny,enum=reject"`
	// Direction selects which traffic the rule matches. It defaults to in.
	Direction string `json:"direction,omitempty" jsonschema:"enum=in,enum=out,enum=forward,default=in"`
	// Source is the address or CIDR the traffic comes from.
	Source string `json:"source,omitempty" jsonschema:"example=10.0.0.0/8"`
	// Destination is the address or CIDR the traffic goes to.
	Destination string `json:"destination,omitempty" jsonschema:"example=10.42.0.0/16"`
	// Ingress names where forward traffic enters. It is a zone on firewalld hosts and an
	// interface on ufw hosts. It applies to forward rules only.
	Ingress string `json:"ingress,omitempty" jsonschema:"example=public"`
	// Egress names where forward traffic leaves. It is a zone on firewalld hosts and an
	// interface on ufw hosts. It applies to forward rules only.
	Egress string `json:"egress,omitempty" jsonschema:"example=trusted"`
	// Port is the port number, or inclusive port range, the rule matches.
	Port string `json:"port,omitempty" jsonschema:"oneof_type=string;integer,example=6443,example=30000-32767"`
	// Protocol is the type of traffic the rule matches.
	Protocol string `json:"protocol,omitempty" jsonschema:"enum=tcp,enum=udp,enum=sctp,enum=dccp"`
}

// NormalizedAction returns the rule's action, lowercased.
func (r ZarfFirewallRule) NormalizedAction() string {
	return strings.ToLower(strings.TrimSpace(r.Action))
}

// NormalizedDirection returns the rule's direction, lowercased, defaulting to in.
func (r ZarfFirewallRule) NormalizedDirection() string {
	d := strings.ToLower(strings.TrimSpace(r.Direction))
	if d == "" {
		return FirewallDirectionIn
	}
	return d
}

// NormalizedProtocol returns the rule's protocol, lowercased.
func (r ZarfFirewallRule) NormalizedProtocol() string {
	return strings.ToLower(strings.TrimSpace(r.Protocol))
}

// Key returns the rule's Name, or a stable name derived from the rule's fields when Name is
// empty. Backends use it to name the files and comments that tie an applied rule back to
// this configuration.
func (r ZarfFirewallRule) Key() string {
	if r.Name != "" {
		return sanitizeName(r.Name)
	}

	parts := []string{r.NormalizedDirection(), r.NormalizedAction()}
	for _, p := range []string{r.Source, r.Destination, r.Ingress, r.Egress, r.Port, r.NormalizedProtocol()} {
		if p != "" {
			parts = append(parts, p)
		}
	}

	return sanitizeName(strings.Join(parts, "-"))
}

// Validate returns an error when the rule cannot be applied by any backend.
func (r ZarfFirewallRule) Validate() error {
	switch r.NormalizedAction() {
	case FirewallActionAllow, FirewallActionDeny, FirewallActionReject:
	case "":
		return fmt.Errorf("firewall rule %q: action is required", r.Key())
	default:
		return fmt.Errorf("firewall rule %q: unknown action %q, want one of allow, deny, reject", r.Key(), r.Action)
	}

	direction := r.NormalizedDirection()
	switch direction {
	case FirewallDirectionIn, FirewallDirectionOut:
		if r.Ingress != "" || r.Egress != "" {
			return fmt.Errorf("firewall rule %q: ingress and egress apply to forward rules only", r.Key())
		}
	case FirewallDirectionForward:
		if r.Ingress == "" || r.Egress == "" {
			return fmt.Errorf("firewall rule %q: forward rules need both ingress and egress", r.Key())
		}
	default:
		return fmt.Errorf("firewall rule %q: unknown direction %q, want one of in, out, forward", r.Key(), r.Direction)
	}

	switch r.NormalizedProtocol() {
	case "", "tcp", "udp", "sctp", "dccp":
	default:
		return fmt.Errorf("firewall rule %q: unknown protocol %q, want one of tcp, udp, sctp, dccp", r.Key(), r.Protocol)
	}

	if err := validatePort(r.Key(), r.Port); err != nil {
		return err
	}
	if r.Port != "" && r.NormalizedProtocol() == "" {
		return fmt.Errorf("firewall rule %q: a port match needs a protocol", r.Key())
	}

	for field, addr := range map[string]string{"source": r.Source, "destination": r.Destination} {
		if err := validateAddress(r.Key(), field, addr); err != nil {
			return err
		}
	}

	return nil
}

// Merge adds every rule from update that c does not already define, matching rules by Key. A
// host that names a rule the same as its profile does keeps its own; every other profile rule
// is added. Unlike the rest of ZarfHostConfig, firewall rules union rather than replace, so a
// host can add one rule of its own without giving up the baseline its profile sets.
func (c *ZarfFirewallConfig) Merge(update ZarfFirewallConfig) {
	if len(update.Rules) == 0 {
		return
	}

	defined := make(map[string]struct{}, len(c.Rules)+len(update.Rules))
	for _, rule := range c.Rules {
		defined[rule.Key()] = struct{}{}
	}

	for _, rule := range update.Rules {
		key := rule.Key()
		if _, exists := defined[key]; exists {
			continue
		}
		defined[key] = struct{}{}
		c.Rules = append(c.Rules, rule)
	}
}

// Validate returns an error when any rule in the config is unusable.
func (c ZarfFirewallConfig) Validate() error {
	seen := make(map[string]struct{}, len(c.Rules))
	for _, rule := range c.Rules {
		if err := rule.Validate(); err != nil {
			return err
		}
		key := rule.Key()
		if _, dup := seen[key]; dup {
			return fmt.Errorf("firewall rule %q: name is not unique", key)
		}
		seen[key] = struct{}{}
	}

	return nil
}

// validatePort checks a port number or inclusive range.
func validatePort(key, port string) error {
	if port == "" {
		return nil
	}
	if !portPattern.MatchString(port) {
		return fmt.Errorf("firewall rule %q: invalid port %q, want a port or a low-high range", key, port)
	}

	low, high, isRange := strings.Cut(port, "-")
	lowNum, err := strconv.Atoi(low)
	if err != nil || lowNum < 1 || lowNum > 65535 {
		return fmt.Errorf("firewall rule %q: port %q is out of range", key, port)
	}
	if !isRange {
		return nil
	}

	highNum, err := strconv.Atoi(high)
	if err != nil || highNum < 1 || highNum > 65535 {
		return fmt.Errorf("firewall rule %q: port %q is out of range", key, port)
	}
	if highNum <= lowNum {
		return fmt.Errorf("firewall rule %q: port range %q does not ascend", key, port)
	}

	return nil
}

// validateAddress checks an address or CIDR match field.
func validateAddress(key, field, addr string) error {
	if addr == "" || strings.EqualFold(addr, "any") {
		return nil
	}
	if strings.Contains(addr, "/") {
		if _, _, err := net.ParseCIDR(addr); err != nil {
			return fmt.Errorf("firewall rule %q: invalid %s CIDR %q", key, field, addr)
		}
		return nil
	}
	if net.ParseIP(addr) == nil {
		return fmt.Errorf("firewall rule %q: invalid %s address %q", key, field, addr)
	}

	return nil
}

// sanitizeName reduces s to characters that are safe in a file name and in a firewall comment.
func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == '.' || r == '/' || r == ' ' || r == ':':
			b.WriteRune('-')
		}
	}

	return strings.Trim(b.String(), "-")
}
