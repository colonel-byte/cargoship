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

// Test_67_UpgradeWorkers covers phase/67_upgrade_worker.go. As with the controller upgrade
// phase, a fresh install leaves it nothing to claim, so the assertion is that it skips and
// leaves the agents Test_62 started running.
func (s *ApplyPhaseSuite) Test_67_UpgradeWorkers() {
	s.requireEngine()
	p := &phase.UpgradeWorkers{
		UpgradeHosts:     phase.UpgradeHosts{Distro: s.harness.distro},
		WorkerConcurrent: s.harness.opts.WorkerConcurrent,
	}
	s.runPhase(p)
	s.Require().False(ran(p), "the upgrade phase claimed workers that were just installed")

	service := s.harness.distro.GetWorkerService()
	for _, host := range s.harness.workers() {
		s.Require().Truef(host.Configurer.ServiceIsRunning(host, service),
			"%s: %s stopped running", host, service)
	}
}
