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
	"github.com/colonel-byte/cargoship/pkg/firewall"
	"github.com/colonel-byte/cargoship/pkg/phase"
)

// firewallDump maps a backend to the command that prints the rules it is enforcing, so the
// test can check what the node ended up with rather than what the phase said it wrote.
var firewallDump = map[string]string{ //nolint:gochecknoglobals
	// The firewalld dump has to include the ipsets, not only the zone. firewalld's zone
	// listing names a source as `ipset:k8s-nodes` and nothing more, so the peer addresses --
	// which is what this test is looking for -- are one level down, inside the set. Asserting
	// on the zone alone was passing only because no container ever ran firewalld, so the
	// assertion never executed; the first host that did run it failed here.
	//
	// The sets are enumerated rather than named, so that the test does not carry a copy of a
	// name owned by pkg/firewall and drift from it.
	firewall.FirewalldService: "firewall-cmd --list-all --zone=trusted; " +
		"for s in $(firewall-cmd --get-ipsets); do firewall-cmd --info-ipset=\"$s\"; done",
	firewall.UFWService:      "ufw status verbose",
	firewall.NftablesService: "nft list ruleset",
}

// configureFirewall covers phase/26_configure_firewall.go. The phase trusts every
// other node in the cluster on whichever firewall the node runs, so the assertion is that
// each node's own firewall now names every peer's private address -- directly, or through a
// set the firewall resolves, which is why firewallDump is a dump and not a single query. A
// node running no firewall is not a failure -- the phase is meant to skip it -- so the test
// asserts the gate matches what the hosts run and stops there when none do.
//
// Against containers the gate is what gets tested, because nothing in a container runs a
// firewall. Only the microvm backend reaches the assertions below; see
// docs/agent/choice-microvm-backend.md.
//
// Both walks that run this phase assert the same thing, so the body is shared: see phaseWalk.
func (s *phaseWalk) configureFirewall() {
	s.T().Helper()

	backends := make(map[string]firewall.Backend, len(s.harness.hosts()))
	for _, host := range s.harness.hosts() {
		if b := firewall.Select(s.ctx, host).Backend; b != nil {
			backends[host.String()] = b
		}
	}

	p := &phase.ConfigureFirewall{Distro: s.harness.distro, Enabled: s.harness.opts.ModifyFirewall}
	s.runPhase(p)
	s.Require().Equalf(s.harness.opts.ModifyFirewall && len(backends) > 0, ran(p),
		"phase ran=%v with enabled=%v and %d hosts running a firewall",
		ran(p), s.harness.opts.ModifyFirewall, len(backends))

	if !ran(p) {
		return
	}

	for _, host := range s.harness.hosts() {
		backend, ok := backends[host.String()]
		if !ok {
			continue
		}
		cmd, ok := firewallDump[backend.Name()]
		s.Require().Truef(ok, "no dump command known for firewall backend %q", backend.Name())

		rules, err := host.ExecOutput(cmd)
		s.Require().NoErrorf(err, "%s: failed to dump %s rules", host, backend.Name())

		for _, peer := range s.harness.hosts() {
			if peer.String() == host.String() {
				continue
			}
			s.Require().Containsf(rules, peer.PrivateAddress,
				"%s: %s does not trust %s", host, backend.Name(), peer)
		}
	}
}

func (s *ApplyPhaseSuite) Test_26_ConfigureFirewall() {
	s.configureFirewall()
}

// Test_26_ConfigureFirewall is the other one: every established node's own firewall dump has to
// name the new node's address, not just the new node's firewall naming theirs.
func (s *JoinPhaseSuite) Test_26_ConfigureFirewall() {
	s.configureFirewall()
}
