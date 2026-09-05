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
