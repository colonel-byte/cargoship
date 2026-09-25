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
	"regexp"

	"github.com/colonel-byte/cargoship/src/pkg/phase"
)

// sysctlConfPath is where phase/20_prepare_host.go renders the distro's sysctl settings.
const sysctlConfPath = "/etc/sysctl.d/99-cargoship.conf"

// prepareHosts covers phase/20_prepare_host.go. It pushes the distro's environment and sysctl
// settings onto each node, so the assertion is that the rendered file is on every host and
// names every setting the package asked for, at the value it asked for.
//
// The kernel values themselves are deliberately not read back. The nodes are Docker
// containers, which share the host kernel: the sysctls this phase writes are not namespaced,
// so a container cannot change them and `sysctl -n` there reports whatever the machine running
// the tests is set to. Asserting on that would be asserting on the developer's or the runner's
// kernel rather than on anything the phase did -- it fails on a correct phase and passes on a
// broken one whenever the host happens to already match. What the phase controls, and all it
// controls from inside a container, is the file, so that is what is checked. The phase still
// runs `sysctl --system` itself, and a failure there fails the phase and so fails runPhase.
func (s *phaseWalk) prepareHosts() {
	s.T().Helper()

	s.runPhase(&phase.PrepareHosts{})

	sysctls := s.harness.manager.Distro.Spec.Config.OS.Sysctl
	if len(sysctls) == 0 {
		s.T().Skip("the package under test carries no sysctl settings")
	}

	rendered, err := readOnHosts(s.harness.hosts(), sysctlConfPath)
	s.Require().NoError(err)

	for _, host := range s.harness.hosts() {
		content := rendered[host.String()]
		s.Require().NotEmptyf(content, "%s: %s is empty", host, sysctlConfPath)

		for key, want := range sysctls {
			// The phase renders one line per setting and pads it through a tabwriter, so the
			// spacing around the "=" is whatever the longest key in the package made it.
			// Matching the whole line rather than the key keeps the value part of the
			// assertion: a file naming every key at the wrong value would otherwise pass.
			line := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `\s+=\s+` + regexp.QuoteMeta(want) + `\s*$`)
			s.Require().Regexpf(line, content,
				"%s: %s does not set %s to %s", host, sysctlConfPath, key, want)
		}
	}
}

func (s *ApplyPhaseSuite) Test_20_PrepareHosts() {
	s.prepareHosts()
}
