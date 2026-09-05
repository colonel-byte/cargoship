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

// Test_62_InitializeWorkers covers phase/62_initialize_worker.go. It joins the workers to
// the control plane the previous phase started, so the assertion is that the agent service
// is running on every worker and that each one is running the packaged engine version. That
// the workers actually registered as nodes is checked once the whole order is done, in
// Test_ZZ1.
func (s *ApplyPhaseSuite) Test_62_InitializeWorkers() {
	p := &phase.InitializeWorkers{
		Distro:           s.harness.distro,
		WorkerConcurrent: s.harness.opts.WorkerConcurrent,
	}
	s.runPhase(p)
	s.Require().True(ran(p), "the inventory has workers but the phase was skipped")

	service := s.harness.distro.GetWorkerService()
	for _, host := range s.harness.engineWorkers() {
		s.Require().Truef(host.Configurer.ServiceIsRunning(host, service),
			"%s: %s is not running", host, service)

		version, err := s.harness.distro.RunningVersion(*host)
		s.Require().NoErrorf(err, "%s: could not read the running engine version", host)
		s.Require().Equalf(s.harness.manager.Distro.Spec.Version, version,
			"%s: running an engine version the package did not ship", host)
	}
}
