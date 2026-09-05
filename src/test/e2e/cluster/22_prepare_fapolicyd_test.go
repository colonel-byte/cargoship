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
	"github.com/colonel-byte/cargoship/src/pkg/phase"
)

// prepareFapolicy covers phase/22_prepare_fapolicyd.go. Like the SELinux phase it is
// gated on what the hosts run: it only fires where fapolicyd is running and the package
// carries a rule set. The assertion is that the gate matches the hosts, and that the rule
// file lands where it fires.
//
// Both walks that run this phase assert the same thing, so the body is shared: see phaseWalk.
func (s *phaseWalk) prepareFapolicy() {
	s.T().Helper()

	var fapolicyd int
	for _, host := range s.harness.hosts() {
		if host.Configurer.ServiceIsRunning(host, phase.FAPOLICYD) {
			fapolicyd++
		}
	}
	carriesRules := s.harness.manager.Distro.Spec.Config.OS.FAPolicyd != ""

	p := &phase.PrepareFapolicy{}
	s.runPhase(p)
	s.Require().Equalf(fapolicyd > 0 && carriesRules, ran(p),
		"phase ran=%v with %d fapolicyd hosts and rules=%v", ran(p), fapolicyd, carriesRules)

	if !ran(p) {
		return
	}

	for _, host := range s.harness.hosts() {
		if !host.Configurer.ServiceIsRunning(host, phase.FAPOLICYD) {
			continue
		}
		s.Require().Truef(host.FileExist(phase.FAPolicydRuleFile),
			"%s: no rule file at %s", host, phase.FAPolicydRuleFile)
	}
}

func (s *ApplyPhaseSuite) Test_22_PrepareFapolicy() {
	s.prepareFapolicy()
}

// Test_22_PrepareFapolicy re-checks the fapolicyd gate with the new node in the cluster.
func (s *JoinPhaseSuite) Test_22_PrepareFapolicy() {
	s.prepareFapolicy()
}
