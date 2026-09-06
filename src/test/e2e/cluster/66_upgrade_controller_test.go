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

// Test_66_UpgradeController is the point of the upgrade walk. The controllers are running the
// version the install put there and the package carries a newer one, so this is the one place
// phase/66_upgrade_controller.go is asserted to have actually done its work rather than to
// have correctly stayed out of the way.
//
// The phase drains each controller, stops the service, runs the install hook phase/59 staged,
// starts the service again and uncordons the node, one controller at a time. So the assertions
// are all three of those outcomes: the phase claimed the hosts, each one now runs the packaged
// version, and none was left stopped or cordoned.
func (s *UpgradePhaseSuite) Test_66_UpgradeController() {
	p := &phase.UpgradeController{UpgradeHosts: phase.UpgradeHosts{Distro: s.harness.distro}}
	s.runPhase(p)
	s.Require().True(ran(p), "the controllers run an older version but the upgrade phase skipped them")

	service := s.harness.distro.GetControllerService()
	for _, host := range s.harness.controllers() {
		s.Require().Truef(host.Configurer.ServiceIsRunning(host, service),
			"%s: %s did not come back up after the upgrade", host, service)

		version, err := s.harness.distro.RunningVersion(*host)
		s.Require().NoErrorf(err, "%s: could not read the running engine version", host)
		s.Require().Equalf(s.harness.manager.Distro.Spec.Version, version,
			"%s: still running the version the upgrade was meant to replace", host)
	}

	s.requireSchedulable(s.harness.controllers())
}
