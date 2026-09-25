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
	"strings"

	"github.com/colonel-byte/cargoship/src/pkg/phase"
)

// connect covers phase/07_connect.go, the first phase apply runs. Every later phase
// test reuses the SSH connections it opens.
func (s *phaseWalk) connect() {
	s.T().Helper()

	s.runPhase(&phase.Connect{})

	for _, host := range s.harness.hosts() {
		out, err := host.ExecOutput("echo connected")
		s.Require().NoErrorf(err, "%s: not reachable after the connect phase", host)
		s.Require().Equal("connected", strings.TrimSpace(out))
	}
}

func (s *ApplyPhaseSuite) Test_07_Connect() {
	s.connect()
}
