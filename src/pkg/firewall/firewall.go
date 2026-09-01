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

// Package firewall renders cargoship's backend-neutral firewall configuration onto whichever
// host firewall a node runs.
//
// A Plan describes what the cluster needs open on one node: the addresses of its peers, the
// pod and service CIDRs, the ports from the inventory's `.host.ports`, and the rules from the
// inventory's `.host.firewall.rules`. A Backend translates that Plan into one firewall's own
// dialect and applies it. Backends are matched to a node by Detect, so a single inventory can
// target a mix of firewalld and ufw hosts.
package firewall

import (
	"context"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
)

// Plan is the desired firewall state for a single node, expressed without reference to any
// particular firewall implementation.
type Plan struct {
	// NodeAddresses lists the private addresses of every node in the cluster. A node trusts
	// all traffic from these addresses.
	NodeAddresses []string
	// ClusterCIDRs lists the pod and service CIDR blocks the distro engine uses. A node
	// trusts all traffic from these blocks.
	ClusterCIDRs []string
	// Ports lists the ports the node exposes publicly, from the inventory's `.host.ports`.
	Ports []cluster.ZarfHostPort
	// Rules lists the backend-neutral rules from the inventory's `.host.firewall.rules`.
	Rules []cluster.ZarfFirewallRule
	// Policies holds the legacy firewalld-only policies from the inventory's `.host.policy`.
	// Backends other than firewalld ignore it.
	Policies map[string]cluster.ZarfFirewallPolicyConfig
}

// IsEmpty is true when the plan would not change anything on a node.
func (p Plan) IsEmpty() bool {
	return len(p.NodeAddresses) == 0 &&
		len(p.ClusterCIDRs) == 0 &&
		len(p.Ports) == 0 &&
		len(p.Rules) == 0 &&
		len(p.Policies) == 0
}

// Backend applies a Plan to a node using one host firewall implementation.
type Backend interface {
	// Name is the backend identifier, e.g. "firewalld" or "ufw".
	Name() string
	// Detect is true when this backend manages the firewall on h. It runs commands on the
	// host, so callers should call it once per host and reuse the result.
	Detect(h *cluster.ZarfHost) bool
	// Apply makes the node's firewall match p, then reloads the firewall. It is
	// idempotent: applying the same plan twice leaves the node in the same state, and
	// rules cargoship applied on an earlier run that p no longer contains are removed.
	Apply(ctx context.Context, h *cluster.ZarfHost, p Plan) error
}

// backends are matched against a host in order, so the more specific backend comes first.
var backends = []Backend{
	&Firewalld{},
	&UFW{},
}

// For returns the backend that manages h's firewall, or nil when the node runs no firewall
// cargoship knows how to configure.
func For(h *cluster.ZarfHost) Backend {
	for _, b := range backends {
		if b.Detect(h) {
			return b
		}
	}

	return nil
}

// Backends returns the registered backends, in match order.
func Backends() []Backend {
	return append([]Backend(nil), backends...)
}
