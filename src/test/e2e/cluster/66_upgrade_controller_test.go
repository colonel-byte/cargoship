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

// Test_66_UpgradeController covers phase/66_upgrade_controller.go. Apply runs the initialize
// and the upgrade phases in the same list and lets each one's Prepare decide which hosts are
// its own, so on the fresh install this suite just performed the upgrade phase must claim
// none of them. The assertion is that it stays out of the way and leaves the control plane
// Test_61 started running.
func (s *ApplyPhaseSuite) Test_66_UpgradeController() {
	s.requireEngine()
	p := &phase.UpgradeController{UpgradeHosts: phase.UpgradeHosts{Distro: s.harness.distro}}
	s.runPhase(p)
	s.Require().False(ran(p), "the upgrade phase claimed controllers that were just installed")

	service := s.harness.distro.GetControllerService()
	for _, host := range s.harness.controllers() {
		s.Require().Truef(host.Configurer.ServiceIsRunning(host, service),
			"%s: %s stopped running", host, service)
	}
}
