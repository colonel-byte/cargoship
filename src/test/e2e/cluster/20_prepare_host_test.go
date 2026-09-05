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
	"fmt"
	"strings"

	"github.com/colonel-byte/cargoship/src/pkg/phase"
)

// sysctlConfPath is where phase/20_prepare_host.go renders the distro's sysctl settings.
const sysctlConfPath = "/etc/sysctl.d/99-cargoship.conf"

// Test_20_PrepareHosts covers phase/20_prepare_host.go. It pushes the distro's environment
// and sysctl settings onto each node, so the assertion is that the rendered file is on every
// host and that the kernel is actually running the values it names, not just storing them.
func (s *ApplyPhaseSuite) Test_20_PrepareHosts() {
	s.runPhase(&phase.PrepareHosts{})

	sysctls := s.harness.manager.Distro.Spec.Config.OS.Sysctl
	if len(sysctls) == 0 {
		s.T().Skip("the package under test carries no sysctl settings")
	}

	rendered, err := readOnHosts(s.harness.hosts(), sysctlConfPath)
	s.Require().NoError(err)

	for _, host := range s.harness.hosts() {
		content := rendered[host.String()]
		for key, want := range sysctls {
			s.Require().Containsf(content, key, "%s: %s does not mention %s", host, sysctlConfPath, key)

			live, err := host.ExecOutput(fmt.Sprintf("sysctl -n %s", key))
			s.Require().NoErrorf(err, "%s: failed to read sysctl %s", host, key)
			s.Require().Equalf(want, strings.TrimSpace(live),
				"%s: sysctl %s was written but not applied", host, key)
		}
	}
}
