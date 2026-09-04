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
// dialect and applies it. Backends are matched to a node by the node's OS and by Detect, so a
// single inventory can target a mix of firewalld, ufw, and nftables hosts.
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
	// Detect is true when this backend manages the firewall on h, meaning the firewall is
	// installed and running. It runs commands on the host, so callers should call it once per
	// host and reuse the result.
	Detect(h *cluster.ZarfHost) bool
	// Installed is true when this firewall is present on h, whether or not it is running. It
	// separates a host that has the firewall stopped from one that never had it at all.
	Installed(h *cluster.ZarfHost) bool
	// Apply makes the node's firewall match p, then reloads the firewall. It is
	// idempotent: applying the same plan twice leaves the node in the same state, and
	// rules cargoship applied on an earlier run that p no longer contains are removed.
	Apply(ctx context.Context, h *cluster.ZarfHost, p Plan) error
}

// backends are matched against a host in order when the host's OS names no preferred firewall,
// so the more specific backend comes first. Nftables is last: firewalld and ufw are both front
// ends onto nftables, so a host running either would match it as well, and the front end is the
// one an operator expects cargoship to configure.
var backends = []Backend{
	&Firewalld{},
	&UFW{},
	&Nftables{},
}

// Selection is the outcome of matching a host against the registered backends. At most one of
// its fields is set.
type Selection struct {
	// Backend manages the host's firewall and is the one cargoship configures. It is nil when
	// cargoship configures nothing on the host.
	Backend Backend
	// Skipped is the host's preferred firewall, installed but not running. Cargoship makes no
	// change to a host in this state: the operator installed a front end and chose to leave it
	// down, and starting it, or writing rules into the nftables underneath it, would take that
	// decision away from them.
	Skipped Backend
}

// Select returns the backend cargoship configures on h.
//
// The host's OS decides first. An OS module names the firewall front end its distribution ships,
// firewalld on Enterprise Linux and SUSE, ufw on Debian and Ubuntu, and that front end is used
// when it is running. When it is installed but stopped, cargoship leaves the host alone rather
// than reaching past the front end to the nftables underneath it. Only a host whose preferred
// front end is absent, or whose OS ships none, falls through to the ordered Detect match, which
// is how a host that manages nftables directly is picked up.
func Select(h *cluster.ZarfHost) Selection {
	detect := func(b Backend) bool { return b.Detect(h) }
	installed := func(b Backend) bool { return b.Installed(h) }

	return selectBackend(backends, preferredFirewall(h), detect, installed)
}

// selectBackend holds the selection rules, with the host reduced to the name of its preferred
// firewall and two predicates, so the rules can be exercised without a host.
func selectBackend(available []Backend, preferred string, detect, installed func(Backend) bool) Selection {
	for _, b := range available {
		if preferred == "" || b.Name() != preferred {
			continue
		}
		if detect(b) {
			return Selection{Backend: b}
		}
		if installed(b) {
			return Selection{Skipped: b}
		}

		break
	}

	for _, b := range available {
		if detect(b) {
			return Selection{Backend: b}
		}
	}

	return Selection{}
}

// preferredFirewall is the firewall front end h's OS ships, or an empty string when the OS ships
// none or has not been resolved.
func preferredFirewall(h *cluster.ZarfHost) string {
	if h == nil || h.Configurer == nil {
		return ""
	}

	return h.Configurer.PreferredFirewall()
}

// Backends returns the registered backends, in match order.
func Backends() []Backend {
	return append([]Backend(nil), backends...)
}
