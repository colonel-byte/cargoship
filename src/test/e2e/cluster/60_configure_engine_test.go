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

// configureEngine covers phase/60_configure_engine.go. It renders the engine config
// onto each node before anything starts, so the assertion is that the config file exists on
// every host and names that host -- a config written with the wrong node name is the failure
// that only shows up much later, as a node that never joins.
//
// This is also the boundary the upload-only hosts stop at. From here on every phase installs,
// starts or queries the engine, and rke2 links against glibc, so the Alpine host cannot run
// it. Dropping the hosts from the manager rather than skipping them in each assertion is what
// makes that hold: InitializeWorkers claims every worker whose agent is not already running,
// with no OS gate, so a test that merely declined to assert on the host would still have
// watched the phase try to install rke2 on it.
//
// Both walks assert the same thing here, so the body is shared: see phaseWalk.
func (s *phaseWalk) configureEngine() {
	s.T().Helper()

	s.Require().NotEmpty(s.harness.dropUploadOnlyHosts(),
		"the upload-only hosts were already gone before the engine phases started")

	s.runPhase(&phase.ConfigureEngine{Distro: s.harness.distro})

	configPath := s.harness.distro.ConfigPath()
	rendered, err := readOnHosts(s.harness.hosts(), configPath)
	s.Require().NoError(err)

	for _, host := range s.harness.hosts() {
		content := rendered[host.String()]
		s.Require().NotEmptyf(content, "%s: %s is empty", host, configPath)
		s.Require().Containsf(content, host.Hostname,
			"%s: %s does not name this node", host, configPath)
		s.Require().Containsf(content, s.harness.distro.DataDirPath(),
			"%s: %s does not point at the engine data directory", host, configPath)
	}
}

// Test_60_ConfigureEngine renders the engine config before the cluster is started.
func (s *ApplyPhaseSuite) Test_60_ConfigureEngine() {
	s.configureEngine()
}

// Test_60_ConfigureEngine re-renders the engine config from the newer package before the
// upgrade phases restart the services onto it.
func (s *UpgradePhaseSuite) Test_60_ConfigureEngine() {
	s.configureEngine()
}

// Test_60_ConfigureEngine renders the engine config on the joining machine before it is
// started, and re-renders it on the nodes already running. The assertion that each config
// names its own host is what rules out the failure this phase is most able to cause on a join:
// a new node handed a config naming a node that already exists.
func (s *JoinPhaseSuite) Test_60_ConfigureEngine() {
	s.configureEngine()
}
