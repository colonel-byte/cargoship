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

// engineConfigSyncWorker covers phase/72_engine_config_sync_worker.go, the worker
// half of the drift check. As with the controller half, a cluster this suite just configured
// has nothing to sync.
func (s *phaseWalk) engineConfigSyncWorker() {
	s.T().Helper()

	p := &phase.EngineConfigSyncWorker{
		EngineConfigSyncHosts: phase.EngineConfigSyncHosts{Distro: s.harness.distro},
		WorkerConcurrent:      s.harness.opts.WorkerConcurrent,
	}
	s.runPhase(p)
	s.Require().False(ran(p), "config drift reported on workers configured moments ago")

	service := s.harness.distro.GetWorkerService()
	for _, host := range s.harness.workers() {
		s.Require().Truef(host.Configurer.ServiceIsRunning(host, service),
			"%s: %s stopped running", host, service)
	}
}

// Test_72_EngineConfigSyncWorker checks the worker half after the install.
func (s *ApplyPhaseSuite) Test_72_EngineConfigSyncWorker() {
	s.requireEngine()
	s.engineConfigSyncWorker()
}
