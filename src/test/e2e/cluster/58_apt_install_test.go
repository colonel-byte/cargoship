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
	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/pkg/phase"
	"github.com/colonel-byte/cargoship/src/pkg/utils"
)

// aptUploadFiles covers phase/58_apt_install.go, the mirror of the RPM phase for the
// Debian side of the cluster: it claims the Ubuntu replicas and must leave the Fedora ones
// untouched. As with RPM, an rke2 package carries no .deb, so the phase is expected to record
// nothing -- what it must never do is drop what phase/50 already staged.
//
// Both walks assert the same thing here, so the body is shared: see phaseWalk.
func (s *phaseWalk) aptUploadFiles() {
	s.T().Helper()

	debian := s.harness.hosts().Filter(utils.FilterDebianLinux)
	s.Require().NotEmpty(debian,
		"no Debian host in the cluster, the APT phase would be untested")

	isDebian := make(map[string]bool, len(debian))
	for _, host := range debian {
		isDebian[host.String()] = true
	}

	before := make(map[string][]string, len(s.harness.hosts()))
	for _, host := range s.harness.hosts() {
		before[host.String()] = s.manifestOn(host)
	}

	s.runPhase(&phase.APTUploadFiles{})

	for _, host := range s.harness.hosts() {
		after := s.manifestOn(host)
		if !isDebian[host.String()] || !s.harness.carriesFilesFor(config.SelectorAPT) {
			s.Require().Equalf(before[host.String()], after,
				"%s: the APT phase changed a host it does not claim", host)
			continue
		}
		s.Require().Subsetf(after, before[host.String()],
			"%s: the APT phase dropped files an earlier upload phase staged", host)
	}

	if s.harness.carriesFilesFor(config.SelectorAPT) {
		return
	}

	// A phase claims a host by setting EngineUploaded, which is what the later upload phases
	// filter on. Claiming a Debian host this package has no .deb for locks phase/59, the
	// catch-all, out of the one host it exists to serve, and the host reaches the initialize
	// phases with no engine on it at all.
	for _, host := range debian {
		s.Require().Falsef(host.Metadata.EngineUploaded,
			"%s: the APT phase claimed a host it uploaded nothing to", host)
	}
}

// Test_58_APTUploadFiles routes the install's APT uploads.
func (s *ApplyPhaseSuite) Test_58_APTUploadFiles() {
	s.aptUploadFiles()
}

// Test_58_APTUploadFiles routes the join's APT uploads, on the same split: the joining machine
// runs Fedora, so this phase has to leave it alone.
func (s *JoinPhaseSuite) Test_58_APTUploadFiles() {
	s.aptUploadFiles()
}
