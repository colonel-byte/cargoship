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

// Test_61_InitializeControllers covers phase/61_initialize_controller.go. It runs the
// install hook phase/59 left on each controller and brings the control-plane service up, so
// the assertion is that the service is running on every controller, that the join token the
// workers need in Test_62 exists, and that the running version is the one the package
// carries.
func (s *ApplyPhaseSuite) Test_61_InitializeControllers() {
	p := &phase.InitializeControllers{Distro: s.harness.distro}
	s.runPhase(p)
	s.Require().True(ran(p), "the inventory has controllers but the phase was skipped")

	service := s.harness.distro.GetControllerService()
	for _, host := range s.harness.controllers() {
		s.Require().Truef(host.Configurer.ServiceIsRunning(host, service),
			"%s: %s is not running", host, service)

		token := s.harness.distro.JoinTokenPath()
		s.Require().Truef(host.FileExist(token), "%s: no join token at %s", host, token)

		version, err := s.harness.distro.RunningVersion(*host)
		s.Require().NoErrorf(err, "%s: could not read the running engine version", host)
		s.Require().Equalf(s.harness.manager.Distro.Spec.Version, version,
			"%s: running an engine version the package did not ship", host)
	}
}

// Test_61_InitializeControllers, on the upgrade walk, must claim nothing. The phase's Prepare
// takes controllers whose service is not running and whose version is unknown, so a controller
// that is up and reporting a version is not its business -- and if it were, this phase would
// re-bootstrap a live control plane rather than upgrade it. The assertion is that it skips and
// that the control plane is still up for Test_66 to upgrade.
func (s *UpgradePhaseSuite) Test_61_InitializeControllers() {
	p := &phase.InitializeControllers{Distro: s.harness.distro}
	s.runPhase(p)
	s.Require().False(ran(p), "the initialize phase claimed controllers that are already running")

	service := s.harness.distro.GetControllerService()
	for _, host := range s.harness.controllers() {
		s.Require().Truef(host.Configurer.ServiceIsRunning(host, service),
			"%s: %s stopped running", host, service)

		version, err := s.harness.distro.RunningVersion(*host)
		s.Require().NoErrorf(err, "%s: could not read the running engine version", host)
		s.Require().Equalf(installedVersion, version,
			"%s: something upgraded the engine before the upgrade phase ran", host)
	}
}

// Test_61_InitializeControllers, on the join walk, must claim nothing. The join adds a worker,
// and every controller is up and reporting a version, which is what the phase's Prepare
// excludes -- claiming one here would re-bootstrap a live control plane in order to add a node
// to it.
func (s *JoinPhaseSuite) Test_61_InitializeControllers() {
	p := &phase.InitializeControllers{Distro: s.harness.distro}
	s.runPhase(p)
	s.Require().False(ran(p), "the initialize phase claimed controllers that are already running")

	service := s.harness.distro.GetControllerService()
	for _, host := range s.harness.controllers() {
		s.Require().Truef(host.Configurer.ServiceIsRunning(host, service),
			"%s: %s stopped running", host, service)

		version, err := s.harness.distro.RunningVersion(*host)
		s.Require().NoErrorf(err, "%s: could not read the running engine version", host)
		s.Require().Equalf(installedVersion, version,
			"%s: the join changed the engine version on a controller", host)
	}
}
