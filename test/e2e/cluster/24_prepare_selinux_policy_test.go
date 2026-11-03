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
	"github.com/colonel-byte/cargoship/pkg/phase"
)

// prepareSelinuxPolicy covers phase/24_prepare_selinux_policy.go. The gate has two halves, and
// which half is the deciding one varies by environment: a container with no SELinux of its own
// fails the host half no matter what the package carries, and a package with no selinux block
// fails the policy half even on an enforcing host. The assertion is that the phase agrees with
// both, and that a run leaves the state file the reset phase reads.
//
// Both walks that run this phase assert the same thing, so the body is shared: see phaseWalk.
func (s *phaseWalk) prepareSelinuxPolicy() {
	s.T().Helper()

	var capable int
	for _, host := range s.harness.hosts() {
		if selinuxEnabled(host) && host.FS().CommandExist("semodule") {
			capable++
		}
	}
	carriesPolicy := !s.harness.manager.Distro.Spec.Config.OS.SELinux.IsZero()

	p := &phase.PrepareSelinuxPolicy{}
	s.runPhase(p)
	s.Require().Equalf(capable > 0 && carriesPolicy, ran(p),
		"phase ran=%v with %d policy-capable hosts and policy=%v", ran(p), capable, carriesPolicy)

	if !ran(p) {
		return
	}

	for _, host := range s.harness.hosts() {
		if !selinuxEnabled(host) || !host.FS().CommandExist("semodule") {
			continue
		}
		s.Require().Truef(host.FileExist(phase.SELinuxStateFile),
			"%s: no selinux state file at %s", host, phase.SELinuxStateFile)
	}
}

func (s *ApplyPhaseSuite) Test_24_PrepareSelinuxPolicy() {
	s.prepareSelinuxPolicy()
}

// Test_24_PrepareSelinuxPolicy re-checks the SELinux policy gate with the new node in the cluster.
func (s *JoinPhaseSuite) Test_24_PrepareSelinuxPolicy() {
	s.prepareSelinuxPolicy()
}
