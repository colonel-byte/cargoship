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
	s.requireEngine()
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

// Test_62_InitializeWorkers, on the upgrade walk, must claim nothing, for the same reason the
// controller half must not: the workers are already joined and running, and re-running the
// join would tear a working node out of the cluster rather than upgrade it.
func (s *UpgradePhaseSuite) Test_62_InitializeWorkers() {
	p := &phase.InitializeWorkers{
		Distro:           s.harness.distro,
		WorkerConcurrent: s.harness.opts.WorkerConcurrent,
	}
	s.runPhase(p)
	s.Require().False(ran(p), "the initialize phase claimed workers that are already running")

	service := s.harness.distro.GetWorkerService()
	for _, host := range s.harness.workers() {
		s.Require().Truef(host.Configurer.ServiceIsRunning(host, service),
			"%s: %s stopped running", host, service)

		version, err := s.harness.distro.RunningVersion(*host)
		s.Require().NoErrorf(err, "%s: could not read the running engine version", host)
		s.Require().Equalf(installedVersion, version,
			"%s: something upgraded the engine before the upgrade phase ran", host)
	}
}

// Test_62_InitializeWorkers, on the join walk, is the phase that does the joining. It claims
// every worker whose agent service is not already running, so the assertion is that exactly
// one worker is in that state going in -- the machine this walk added -- and that all of them
// are running the packaged version coming out.
//
// The phase keeps the hosts it claimed to itself, so the split is asserted on the state its
// Prepare reads: which agent services are running before the phase, and which after.
func (s *JoinPhaseSuite) Test_62_InitializeWorkers() {
	service := s.harness.distro.GetWorkerService()

	for _, host := range s.harness.engineWorkers() {
		running := host.Configurer.ServiceIsRunning(host, service)
		if s.isJoined(host) {
			s.Require().Falsef(running,
				"%s: the machine being joined is already running %s, so this phase has nothing to join",
				host, service)
			continue
		}
		s.Require().Truef(running, "%s: %s stopped running before the join phase", host, service)
	}

	p := &phase.InitializeWorkers{
		Distro:           s.harness.distro,
		WorkerConcurrent: s.harness.opts.WorkerConcurrent,
	}
	s.runPhase(p)
	s.Require().True(ran(p), "the joining machine is not running the agent but the phase was skipped")

	for _, host := range s.harness.engineWorkers() {
		s.Require().Truef(host.Configurer.ServiceIsRunning(host, service),
			"%s: %s is not running", host, service)

		version, err := s.harness.distro.RunningVersion(*host)
		s.Require().NoErrorf(err, "%s: could not read the running engine version", host)
		s.Require().Equalf(s.harness.manager.Distro.Spec.Version, version,
			"%s: running an engine version the package did not ship", host)
	}
}
