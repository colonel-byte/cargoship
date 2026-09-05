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
	"net"

	"github.com/colonel-byte/cargoship/src/pkg/phase"
)

// Test_10_GatherFacts covers phase/10_gather_facts.go. It fills in the host metadata the
// later phases key off -- hostname, architecture, private address -- so the assertion is
// that every host came back with all three and that they match what bootloose provisioned.
func (s *ApplyPhaseSuite) Test_10_GatherFacts() {
	s.runPhase(&phase.GatherFacts{})

	for _, host := range s.harness.hosts() {
		s.Require().Equalf(host.Hostname, host.Metadata.Hostname,
			"%s: gathered hostname does not match the one the inventory pinned", host)

		s.Require().NotEmptyf(host.Metadata.Arch, "%s: no architecture detected", host)
		s.Require().Equal(e2e.Arch, host.Metadata.Arch)

		s.Require().NotEmptyf(host.PrivateAddress, "%s: no private address discovered", host)
		s.Require().NotNilf(net.ParseIP(host.PrivateAddress),
			"%s: private address %q is not an IP", host, host.PrivateAddress)
		s.Require().NotEmptyf(host.PrivateInterface, "%s: no private interface discovered", host)
	}
}
