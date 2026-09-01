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
	"fmt"
	"net"
	"slices"
	"sort"
	"strings"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/k0sproject/rig/exec"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	// NftablesService is the name of the nftables system service.
	NftablesService = "nftables"
	// nftTable is the single table cargoship owns on an nftables host. Nothing outside it is
	// ever read or written, so kube-proxy's and the CNI's tables are left alone.
	nftTable = "cargoship"
	// nftFamily is the address family of that table. inet covers IPv4 and IPv6 in one table.
	nftFamily = "inet"
	// nftRulesetDir holds the ruleset cargoship renders for this node.
	nftRulesetDir = "/etc/cargoship"
	// nftRulesetPath is the rendered ruleset. It is the whole of cargoship's desired state,
	// loaded in one transaction, which is what makes an apply idempotent without a separate
	// record of what the last run did.
	nftRulesetPath = nftRulesetDir + "/nftables.nft"
	// nftComment prefixes the comment cargoship stamps on every rule it owns, so an operator
	// reading `nft list ruleset` can tell cargoship's rules from their own.
	nftComment = "cargoship"
	// nftClusterSetV4 is the named set holding the cluster's trusted IPv4 addresses and CIDRs.
	nftClusterSetV4 = "cluster_v4"
	// nftClusterSetV6 is the named set holding the cluster's trusted IPv6 addresses and CIDRs.
	nftClusterSetV6 = "cluster_v6"
)

// nftConfPaths are the boot-time ruleset files the nftables service reads, in the order
// cargoship looks for one to add its include line to. Debian, Arch, and Alpine use the first;
// Enterprise Linux and SUSE use the second.
var nftConfPaths = []string{
	"/etc/nftables.conf",
	"/etc/sysconfig/nftables.conf",
}

// Nftables applies a Plan to a node that manages its firewall with nftables directly, rather
// than through firewalld or ufw. It is the backend for hosts that never had either -- Debian
// and Arch nodes configured by hand, and the minimal or immutable images (CoreOS, Flatcar)
// that ship no firewall front end at all.
//
// Everything cargoship writes lives in one table, `inet cargoship`, which is replaced whole in
// a single nft transaction on every apply. Cargoship never flushes the ruleset and never
// touches another table, because kube-proxy and every CNI keep their own service and policy
// rules in this same subsystem; a global flush would break cluster networking until they
// resynced.
//
// Two things differ from the other backends. Every base chain cargoship writes has an accept
// policy: a default-drop policy applied from a remote phase would cut the connection cargoship
// is running over, the same reason cargoship never runs `ufw enable`. And an accept here ends
// only cargoship's chain, not the hook, so a rule in an operator's own table can still drop
// traffic cargoship allowed -- unlike firewalld's trusted zone, cargoship's trust is not the
// last word on a packet.
type Nftables struct{}

var _ Backend = (*Nftables)(nil)

// Name is the backend identifier.
func (n *Nftables) Name() string {
	return NftablesService
}

// Detect is true when the node persists an nftables ruleset of its own, meaning the nftables
// service is running or one of the distro ruleset files exists.
//
// A node whose only nftables content comes from kube-proxy and the CNI is deliberately not a
// match. Every node in a running cluster has a non-empty ruleset, so matching on that would
// claim hosts whose operator never configured a firewall at all.
func (n *Nftables) Detect(h *cluster.ZarfHost) bool {
	if h == nil || h.Configurer == nil || !h.Configurer.CommandExist(h, "nft") {
		return false
	}

	if h.Configurer.ServiceIsRunning(h, NftablesService) {
		return true
	}

	return nftConfPath(h) != ""
}

// Apply renders p as a complete ruleset, checks it, and loads it in one transaction. The
// table is replaced rather than edited, so rules cargoship applied on an earlier run that p no
// longer contains are gone once the transaction commits.
func (n *Nftables) Apply(ctx context.Context, h *cluster.ZarfHost, p Plan) error {
	ruleset, err := nftRuleset(p)
	if err != nil {
		return err
	}

	if err := h.Configurer.MkDir(h, nftRulesetDir, exec.Sudo(h)); err != nil {
		return err
	}
	if err := h.WriteFile(nftRulesetPath, ruleset, "0600"); err != nil {
		return err
	}

	// -c parses and validates without loading. A rendering bug that would otherwise land a
	// broken ruleset on a node cargoship can no longer reach is worth one extra round trip.
	if err := h.Exec("nft -c -f "+nftRulesetPath, exec.Sudo(h)); err != nil {
		return fmt.Errorf("generated nftables ruleset was rejected by nft: %w", err)
	}

	if err := h.Exec("nft -f "+nftRulesetPath, exec.Sudo(h)); err != nil {
		return err
	}

	return n.persist(ctx, h)
}

// persist adds an include for cargoship's ruleset to the distro's boot-time nftables file, so
// the rules survive a reboot. The operator's own file is never rewritten, only appended to,
// and only once.
func (n *Nftables) persist(ctx context.Context, h *cluster.ZarfHost) error {
	conf := nftConfPath(h)
	if conf == "" {
		logger.From(ctx).Warn("no nftables ruleset file on this host, cargoship's rules will not survive a reboot",
			"host", h.String(), "lookedIn", strings.Join(nftConfPaths, ", "))

		return nil
	}

	include := nftInclude()
	if h.Configurer.FileContains(h, conf, include) {
		return nil
	}

	content, err := h.ReadFile(conf)
	if err != nil {
		return err
	}

	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	return h.WriteFile(conf, content+include+"\n", "0600")
}

// nftConfPath returns the boot-time nftables file this host has, or an empty string when it
// has none.
func nftConfPath(h *cluster.ZarfHost) string {
	for _, path := range nftConfPaths {
		if h.FileExist(path) {
			return path
		}
	}

	return ""
}

// nftInclude is the line cargoship adds to the host's boot-time nftables file.
func nftInclude() string {
	return fmt.Sprintf("include %q", nftRulesetPath)
}

// nftRuleset renders a plan as a complete nft script: the table is added, deleted, and
// recreated, which is how nft expresses "replace this table" in one atomic transaction. The
// add is what keeps the delete from failing on a node cargoship has not configured before.
func nftRuleset(p Plan) (string, error) {
	var b strings.Builder

	fmt.Fprintf(&b, "#!/usr/sbin/nft -f\n")
	fmt.Fprintf(&b, "# Managed by cargoship. Edits are replaced on the next apply.\n")
	fmt.Fprintf(&b, "add table %s %s\n", nftFamily, nftTable)
	fmt.Fprintf(&b, "delete table %s %s\n", nftFamily, nftTable)

	if p.IsEmpty() {
		return b.String(), nil
	}

	fmt.Fprintf(&b, "table %s %s {\n", nftFamily, nftTable)
	b.WriteString(nftSets(p))

	chains, err := nftChainRules(p)
	if err != nil {
		return "", err
	}

	for _, chain := range []string{"input", "forward", "output"} {
		fmt.Fprintf(&b, "\tchain %s {\n", chain)
		fmt.Fprintf(&b, "\t\ttype filter hook %s priority filter; policy accept;\n", chain)
		for _, rule := range chains[chain] {
			fmt.Fprintf(&b, "\t\t%s\n", rule)
		}
		b.WriteString("\t}\n")
	}

	b.WriteString("}\n")

	return b.String(), nil
}

// nftSets renders the named sets holding the cluster's trusted addresses and the inventory's
// exposed ports. Sets carry the interval flag so a CIDR or a port range is a single element.
func nftSets(p Plan) string {
	var b strings.Builder

	trusted := append(slices.Clone(p.NodeAddresses), p.ClusterCIDRs...)
	v4, v6 := nftSplitFamilies(trusted)

	for _, set := range []struct {
		name    string
		kind    string
		entries []string
	}{
		{nftClusterSetV4, "ipv4_addr", v4},
		{nftClusterSetV6, "ipv6_addr", v6},
	} {
		if len(set.entries) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\tset %s {\n\t\ttype %s\n\t\tflags interval\n\t\telements = { %s }\n\t}\n",
			set.name, set.kind, strings.Join(set.entries, ", "))
	}

	for _, proto := range nftSortedProtocols(p.Ports) {
		fmt.Fprintf(&b, "\tset %s {\n\t\ttype inet_service\n\t\tflags interval\n\t\telements = { %s }\n\t}\n",
			nftPortSet(proto), strings.Join(nftPortsFor(p.Ports, proto), ", "))
	}

	return b.String()
}

// nftChainRules renders the plan's rules, grouped by the base chain each one hooks into. The
// cluster trust and exposed ports go in front of the inventory's own rules, so a node can
// always reach its peers regardless of what else the inventory asks for.
func nftChainRules(p Plan) (map[string][]string, error) {
	chains := map[string][]string{}

	trusted := append(slices.Clone(p.NodeAddresses), p.ClusterCIDRs...)
	v4, v6 := nftSplitFamilies(trusted)
	if len(v4) > 0 {
		chains["input"] = append(chains["input"],
			fmt.Sprintf("ip saddr @%s accept comment %q", nftClusterSetV4, nftComment+":cluster"))
	}
	if len(v6) > 0 {
		chains["input"] = append(chains["input"],
			fmt.Sprintf("ip6 saddr @%s accept comment %q", nftClusterSetV6, nftComment+":cluster"))
	}

	for _, proto := range nftSortedProtocols(p.Ports) {
		chains["input"] = append(chains["input"],
			fmt.Sprintf("%s dport @%s accept comment %q", proto, nftPortSet(proto), nftComment+":ports"))
	}

	for _, rule := range p.Rules {
		chain, line, err := nftRule(rule)
		if err != nil {
			return nil, err
		}
		chains[chain] = append(chains[chain], line)
	}

	return chains, nil
}

// nftRule renders a single neutral rule, returning the base chain it belongs in along with
// the rule itself.
func nftRule(rule cluster.ZarfFirewallRule) (string, string, error) {
	verdict, err := nftVerdict(rule.NormalizedAction())
	if err != nil {
		return "", "", err
	}

	var (
		chain string
		parts []string
	)

	switch rule.NormalizedDirection() {
	case cluster.FirewallDirectionForward:
		chain = "forward"
		parts = append(parts, fmt.Sprintf("iifname %q", rule.Ingress), fmt.Sprintf("oifname %q", rule.Egress))
	case cluster.FirewallDirectionOut:
		chain = "output"
	default:
		chain = "input"
	}

	if rule.Source != "" {
		parts = append(parts, fmt.Sprintf("%s saddr %s", nftAddrFamily(rule.Source), rule.Source))
	}
	if rule.Destination != "" {
		parts = append(parts, fmt.Sprintf("%s daddr %s", nftAddrFamily(rule.Destination), rule.Destination))
	}

	switch proto := rule.NormalizedProtocol(); {
	case proto != "" && rule.Port != "":
		parts = append(parts, fmt.Sprintf("%s dport %s", proto, nftPortRange(rule.Port)))
	case proto != "":
		parts = append(parts, "meta l4proto "+proto)
	}

	parts = append(parts, verdict, fmt.Sprintf("comment %q", nftComment+":"+rule.Key()))

	return chain, strings.Join(parts, " "), nil
}

// nftVerdict converts a neutral action into the nftables statement that carries it out.
func nftVerdict(action string) (string, error) {
	switch action {
	case cluster.FirewallActionAllow:
		return "accept", nil
	case cluster.FirewallActionDeny:
		return "drop", nil
	case cluster.FirewallActionReject:
		return "reject", nil
	default:
		return "", fmt.Errorf("firewall rule: unknown action %q, want one of allow, deny, reject", action)
	}
}

// nftAddrFamily returns the family keyword an address match needs, so that an IPv6 rule is
// written with ip6 rather than ip in the table's shared inet family.
func nftAddrFamily(addr string) string {
	if strings.Contains(addr, ":") {
		return "ip6"
	}

	return "ip"
}

// nftSplitFamilies sorts addresses and CIDRs into their families, dropping anything that
// parses as neither. Order is preserved so the rendered ruleset is stable across runs.
func nftSplitFamilies(addrs []string) (v4 []string, v6 []string) {
	for _, addr := range addrs {
		if addr == "" {
			continue
		}

		ip := net.ParseIP(addr)
		if ip == nil {
			parsed, _, err := net.ParseCIDR(addr)
			if err != nil {
				continue
			}
			ip = parsed
		}

		if ip.To4() != nil {
			v4 = append(v4, addr)

			continue
		}

		v6 = append(v6, addr)
	}

	return v4, v6
}

// nftSortedProtocols returns the protocols the plan's ports use, sorted, so a plan renders the
// same ruleset every time regardless of the order the inventory listed its ports in.
func nftSortedProtocols(ports []cluster.ZarfHostPort) []string {
	seen := map[string]struct{}{}
	for _, port := range ports {
		seen[nftPortProtocol(port)] = struct{}{}
	}

	protocols := make([]string, 0, len(seen))
	for proto := range seen {
		protocols = append(protocols, proto)
	}
	sort.Strings(protocols)

	return protocols
}

// nftPortsFor returns the plan's ports for one protocol, in nftables' range form.
func nftPortsFor(ports []cluster.ZarfHostPort, proto string) []string {
	var out []string
	for _, port := range ports {
		if nftPortProtocol(port) == proto {
			out = append(out, nftPortRange(port.Port))
		}
	}

	return out
}

// nftPortProtocol returns a port's protocol, defaulting to tcp the way the other backends do.
func nftPortProtocol(port cluster.ZarfHostPort) string {
	if proto := strings.ToLower(strings.TrimSpace(port.Protocol)); proto != "" {
		return proto
	}

	return "tcp"
}

// nftPortSet names the set holding the exposed ports for one protocol.
func nftPortSet(proto string) string {
	return "ports_" + proto
}

// nftPortRange returns a port match. nftables writes an inclusive range the same way the
// neutral model does, so only the whitespace needs handling.
func nftPortRange(port string) string {
	return strings.TrimSpace(port)
}
