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

// engineConfigSyncController covers phase/71_engine_config_sync_controller.go. The
// phase exists to catch engine config that has drifted from what the package wants, and
// phase/60 wrote exactly that config a few phases ago, so there is nothing to correct here.
// The assertion is that it detects no drift and restarts nothing -- a sync phase that fired
// on a freshly configured cluster would be bouncing the control plane for no reason.
//
// Both walks assert the same thing here, so the body is shared: see phaseWalk.
func (s *phaseWalk) engineConfigSyncController() {
	s.T().Helper()

	p := &phase.EngineConfigSyncController{
		EngineConfigSyncHosts: phase.EngineConfigSyncHosts{Distro: s.harness.distro},
	}
	s.runPhase(p)
	s.Require().False(ran(p), "config drift reported on controllers configured moments ago")

	service := s.harness.distro.GetControllerService()
	for _, host := range s.harness.controllers() {
		s.Require().Truef(host.Configurer.ServiceIsRunning(host, service),
			"%s: %s stopped running", host, service)
	}
}

// Test_71_EngineConfigSyncController checks for drift after the install.
func (s *ApplyPhaseSuite) Test_71_EngineConfigSyncController() {
	s.engineConfigSyncController()
}

// Test_71_EngineConfigSyncController checks for drift after the upgrade. Test_60 re-rendered
// the config from the newer package, so there is still nothing to correct.
func (s *UpgradePhaseSuite) Test_71_EngineConfigSyncController() {
	s.engineConfigSyncController()
}

// Test_71_EngineConfigSyncController checks for drift after the join. The controllers were not
// reconfigured by it, and Test_60 rendered the same config from the same package, so there is
// still nothing to correct -- a restart of the control plane here would be the join disturbing
// nodes it has no business touching.
func (s *JoinPhaseSuite) Test_71_EngineConfigSyncController() {
	s.engineConfigSyncController()
}
