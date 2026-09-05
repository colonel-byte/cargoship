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

// disconnect covers phase/99_disconnect.go, the last phase in the apply order. It
// clears the temporary binaries the install staged and drops the SSH connections, so the
// assertion is that no host is left holding a staged binary path and that the connections
// really are closed.
//
// Both walks assert the same thing here, so the body is shared: see phaseWalk.
func (s *phaseWalk) disconnect() {
	s.T().Helper()

	s.runPhase(&phase.Disconnect{})

	for _, host := range s.harness.hosts() {
		s.Require().Emptyf(host.Metadata.BinaryTempFile,
			"%s: still tracking staged binaries after disconnect", host)
		s.Require().Falsef(host.Connection.IsConnected(),
			"%s: still connected after the disconnect phase", host)
	}
}

// Test_99_Disconnect closes out the install walk.
func (s *ApplyPhaseSuite) Test_99_Disconnect() {
	s.disconnect()
}

// Test_99_Disconnect closes out the upgrade walk.
func (s *UpgradePhaseSuite) Test_99_Disconnect() {
	s.disconnect()
}
