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
	"slices"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/pkg/phase"
)

// prepareFapolicy covers phase/22_prepare_fapolicyd.go. Like the SELinux phase it is
// gated on what the hosts run: it only fires where fapolicyd is running and the package
// either carries a rule set or declares an executable file, which the phase writes a
// generated allow rule for. The assertion is that the gate matches the hosts, and that
// each rule file lands where the thing that produces it is present.
//
// The generated rules are only reachable with an executable in the package, which on the
// rke2 package means the etcdctl tool writeInventories enables: see the comment there.
//
// Both walks that run this phase assert the same thing, so the body is shared: see phaseWalk.
func (s *phaseWalk) prepareFapolicy() {
	s.T().Helper()

	var fapolicyd int
	for _, host := range s.harness.hosts() {
		if host.ServiceIsRunning(s.ctx, phase.FAPOLICYD) {
			fapolicyd++
		}
	}
	cfg := s.harness.manager.Distro.Spec.Config
	carriesRules := cfg.OS.FAPolicyd != ""
	executables := slices.Concat(cfg.Files, cfg.OS.Files).Filter(func(f *v1alpha1.File) bool {
		return f.Executable && f.Target != ""
	})

	p := &phase.PrepareFapolicy{}
	s.runPhase(p)
	s.Require().Equalf(fapolicyd > 0 && (carriesRules || len(executables) > 0), ran(p),
		"phase ran=%v with %d fapolicyd hosts, rules=%v and %d executables",
		ran(p), fapolicyd, carriesRules, len(executables))

	if !ran(p) {
		return
	}

	for _, host := range s.harness.hosts() {
		if !host.ServiceIsRunning(s.ctx, phase.FAPOLICYD) {
			continue
		}
		if carriesRules {
			s.Require().Truef(host.FileExist(phase.FAPolicydRuleFile),
				"%s: no rule file at %s", host, phase.FAPolicydRuleFile)
		}
		if len(executables) == 0 {
			continue
		}

		s.Require().Truef(host.FileExist(phase.FAPolicydExecutableRuleFile),
			"%s: no generated rule file at %s", host, phase.FAPolicydExecutableRuleFile)

		// Every executable the package declares is named, whatever selector gated its
		// upload: the rules are generated from the package, not from what reached a host.
		written, err := host.Sudo().ExecOutput("cat " + phase.FAPolicydExecutableRuleFile)
		s.Require().NoErrorf(err, "%s: reading %s", host, phase.FAPolicydExecutableRuleFile)
		for _, f := range executables {
			s.Require().Containsf(written, "path="+f.Target,
				"%s: %s names no rule for %s", host, phase.FAPolicydExecutableRuleFile, f.Target)
		}
	}
}

func (s *ApplyPhaseSuite) Test_22_PrepareFapolicy() {
	s.prepareFapolicy()
}

// Test_22_PrepareFapolicy re-checks the fapolicyd gate with the new node in the cluster.
func (s *JoinPhaseSuite) Test_22_PrepareFapolicy() {
	s.prepareFapolicy()
}
