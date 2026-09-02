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
	"slices"
	"strings"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/k0sproject/rig/exec"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	// UFWService is the name of the ufw system service.
	UFWService = "ufw"
	// ufwProfilePath is the application profile cargoship writes the inventory ports into.
	ufwProfilePath = "/etc/ufw/applications.d/cargoship"
	// ufwProfileName is the profile name inside ufwProfilePath, and the name rules refer to.
	ufwProfileName = "cargoship-ports"
	// ufwStateDir holds cargoship's record of the rules it applied to this node.
	ufwStateDir = "/var/lib/cargoship"
	// ufwStatePath is cargoship's record of the rules it applied to this node. It is what
	// makes an apply idempotent: rules recorded here that the current plan dropped are
	// deleted from ufw, and rules the plan still has are re-applied, which ufw ignores when
	// they already exist.
	ufwStatePath = ufwStateDir + "/ufw.rules"
	// ufwComment prefixes the comment cargoship stamps on every rule it owns, so an operator
	// reading `ufw status` can tell cargoship's rules from their own.
	ufwComment = "cargoship"
)

// UFW applies a Plan to a node running ufw.
//
// Cargoship never enables or disables ufw. A node is only configured when ufw is already
// active, because enabling a default-deny firewall from a remote phase would cut the
// connection cargoship is running over.
//
// Two parts of the neutral model mean something slightly different here than they do on
// firewalld: a forward rule's ingress and egress name interfaces rather than zones, since ufw
// has no zones, and a rule with no address match becomes a rule from any address.
type UFW struct{}

var _ Backend = (*UFW)(nil)

// Name is the backend identifier.
func (u *UFW) Name() string {
	return UFWService
}

// Detect is true when ufw is installed on h and reports itself active.
func (u *UFW) Detect(h *cluster.ZarfHost) bool {
	if h == nil || h.Configurer == nil || !h.Configurer.CommandExist(h, UFWService) {
		return false
	}

	out, err := h.ExecOutput("ufw status", exec.Sudo(h))
	if err != nil {
		return false
	}

	return strings.Contains(strings.ToLower(out), "status: active")
}

// Apply reconciles the node's ufw rules with p and reloads ufw. Rules cargoship applied on an
// earlier run that p no longer contains are deleted.
func (u *UFW) Apply(ctx context.Context, h *cluster.ZarfHost, p Plan) error {
	if err := u.applyProfile(h, p); err != nil {
		return err
	}

	desired, err := ufwRules(p)
	if err != nil {
		return err
	}

	previous := u.readState(ctx, h)
	for _, rule := range previous {
		if slices.Contains(desired, rule) {
			continue
		}
		u.deleteRule(ctx, h, rule)
	}

	for _, rule := range desired {
		if err := h.Exec("ufw "+rule, exec.Sudo(h)); err != nil {
			return fmt.Errorf("failed to apply ufw rule %q: %w", rule, err)
		}
	}

	if err := u.writeState(h, desired); err != nil {
		return err
	}

	return h.Exec("ufw --force reload", exec.Sudo(h))
}

// applyProfile writes the application profile holding the inventory ports, and removes it
// when the plan has no ports left.
func (u *UFW) applyProfile(h *cluster.ZarfHost, p Plan) error {
	if len(p.Ports) == 0 {
		if h.FileExist(ufwProfilePath) {
			return h.DeleteFile(ufwProfilePath)
		}

		return nil
	}

	if err := h.WriteFile(ufwProfilePath, ufwProfile(p.Ports), "0644"); err != nil {
		return err
	}

	// ufw caches profiles, so an edited profile only reaches existing rules after an update.
	return h.Exec("ufw app update "+ufwProfileName, exec.Sudo(h))
}

// readState returns the rules cargoship applied to h on its last run. A node cargoship has
// not configured before, or one whose state file is unreadable, is treated as having none.
func (u *UFW) readState(ctx context.Context, h *cluster.ZarfHost) []string {
	if !h.FileExist(ufwStatePath) {
		return nil
	}

	content, err := h.ReadFile(ufwStatePath)
	if err != nil {
		logger.From(ctx).Warn("could not read ufw state, skipping cleanup of stale rules",
			"host", h.String(), "path", ufwStatePath, "error", err)

		return nil
	}

	var rules []string
	for _, line := range strings.Split(content, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			rules = append(rules, line)
		}
	}

	return rules
}

// writeState records the rules cargoship applied, for the next run to reconcile against.
func (u *UFW) writeState(h *cluster.ZarfHost, rules []string) error {
	if err := h.Configurer.MkDir(h, ufwStateDir, exec.Sudo(h)); err != nil {
		return err
	}

	return h.WriteFile(ufwStatePath, strings.Join(rules, "\n")+"\n", "0600")
}

// deleteRule removes a rule cargoship applied on an earlier run. A delete that fails is
// logged rather than returned: the rule may already be gone, and that is not a reason to fail
// the phase.
func (u *UFW) deleteRule(ctx context.Context, h *cluster.ZarfHost, rule string) {
	if err := h.Exec("ufw --force delete "+stripComment(rule), exec.Sudo(h)); err != nil {
		logger.From(ctx).Warn("could not delete stale ufw rule",
			"host", h.String(), "rule", rule, "error", err)
	}
}

// ufwProfile renders the application profile holding the inventory ports.
func ufwProfile(ports []cluster.ZarfHostPort) string {
	specs := make([]string, 0, len(ports))
	for _, port := range ports {
		proto := strings.ToLower(port.Protocol)
		if proto == "" {
			proto = "tcp"
		}
		specs = append(specs, fmt.Sprintf("%s/%s", ufwPortRange(port.Port), proto))
	}

	return fmt.Sprintf(`[%s]
title=Cargoship exposed ports
description=Ports cargoship opens on this node for the cluster
ports=%s
`, ufwProfileName, strings.Join(specs, "|"))
}

// ufwRules renders a plan as ufw command arguments, one per rule, in the order cargoship
// applies them.
func ufwRules(p Plan) ([]string, error) {
	var rules []string

	if len(p.Ports) > 0 {
		rules = append(rules, fmt.Sprintf("allow %s comment '%s:ports'", ufwProfileName, ufwComment))
	}

	for _, addr := range append(slices.Clone(p.NodeAddresses), p.ClusterCIDRs...) {
		if addr == "" {
			continue
		}
		rules = append(rules, fmt.Sprintf("allow from %s comment '%s:cluster'", addr, ufwComment))
	}

	for _, rule := range p.Rules {
		line, err := ufwRule(rule)
		if err != nil {
			return nil, err
		}
		rules = append(rules, line)
	}

	return rules, nil
}

// ufwRule renders a single neutral rule as ufw command arguments.
func ufwRule(rule cluster.ZarfFirewallRule) (string, error) {
	action := rule.NormalizedAction()
	switch action {
	case cluster.FirewallActionAllow, cluster.FirewallActionDeny, cluster.FirewallActionReject:
	default:
		return "", fmt.Errorf("firewall rule %q: unknown action %q", rule.Key(), rule.Action)
	}

	var parts []string
	switch rule.NormalizedDirection() {
	case cluster.FirewallDirectionForward:
		parts = append(parts, "route", action,
			"in", "on", rule.Ingress,
			"out", "on", rule.Egress,
		)
	case cluster.FirewallDirectionOut:
		parts = append(parts, action, "out")
	default:
		parts = append(parts, action, "in")
	}

	parts = append(parts, "from", ufwAddress(rule.Source), "to", ufwAddress(rule.Destination))
	if rule.Port != "" {
		parts = append(parts, "port", ufwPortRange(rule.Port))
	}
	if proto := rule.NormalizedProtocol(); proto != "" {
		parts = append(parts, "proto", proto)
	}
	parts = append(parts, fmt.Sprintf("comment '%s:%s'", ufwComment, rule.Key()))

	return strings.Join(parts, " "), nil
}

// ufwAddress returns an address match, defaulting to ufw's any.
func ufwAddress(addr string) string {
	if addr == "" {
		return "any"
	}

	return addr
}

// ufwPortRange converts a neutral "low-high" range into ufw's "low:high" form and leaves
// single ports alone.
func ufwPortRange(port string) string {
	return strings.ReplaceAll(port, "-", ":")
}

// stripComment removes the trailing comment from a rule, which ufw does not match on when
// deleting a rule.
func stripComment(rule string) string {
	if idx := strings.Index(rule, " comment '"); idx != -1 {
		return rule[:idx]
	}

	return rule
}
