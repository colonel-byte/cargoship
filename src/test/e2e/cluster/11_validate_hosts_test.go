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

// Test_11_ValidateHosts covers phase/11_validate_hosts.go. The phase rejects a cluster whose
// hosts collide or whose architecture the package does not carry, so passing it is most of
// the assertion; the rest re-checks the two properties it enforces against the facts the
// previous phase gathered.
func (s *ApplyPhaseSuite) Test_11_ValidateHosts() {
	s.runPhase(&phase.ValidateHosts{})

	hostnames := make(map[string]int, len(s.harness.hosts()))
	addresses := make(map[string]int, len(s.harness.hosts()))
	for _, host := range s.harness.hosts() {
		hostnames[host.Metadata.Hostname]++
		addresses[host.PrivateAddress]++
	}

	for name, count := range hostnames {
		s.Require().Equalf(1, count, "hostname %q is claimed by %d hosts", name, count)
	}
	for addr, count := range addresses {
		s.Require().Equalf(1, count, "private address %q is claimed by %d hosts", addr, count)
	}
}
