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

// rpmUploadFiles covers phase/57_rpm_install.go. The phase claims only Enterprise
// Linux hosts, which on this cluster means the Fedora replicas, so the assertion is the
// split: the Ubuntu nodes come out byte-identical, and the Fedora nodes keep everything
// phase/50 staged. Whether the Fedora nodes gain anything depends on the package -- an rke2
// package installs from a tarball and carries no RPM -- so that half is asserted against
// what the package actually ships rather than assumed.
//
// Both walks assert the same thing here, so the body is shared: see phaseWalk.
func (s *phaseWalk) rpmUploadFiles() {
	s.T().Helper()

	enterprise := s.harness.hosts().Filter(utils.FilterEnterpriseLinux)
	s.Require().NotEmpty(enterprise,
		"no Enterprise Linux host in the cluster, the RPM phase would be untested")

	isEnterprise := make(map[string]bool, len(enterprise))
	for _, host := range enterprise {
		isEnterprise[host.String()] = true
	}

	before := make(map[string][]string, len(s.harness.hosts()))
	for _, host := range s.harness.hosts() {
		before[host.String()] = s.manifestOn(host)
	}

	s.runPhase(&phase.RPMUploadFiles{})

	for _, host := range s.harness.hosts() {
		after := s.manifestOn(host)
		if !isEnterprise[host.String()] || !s.harness.carriesFilesFor(config.SelectorRPM) {
			s.Require().Equalf(before[host.String()], after,
				"%s: the RPM phase changed a host it does not claim", host)
			continue
		}
		s.Require().Subsetf(after, before[host.String()],
			"%s: the RPM phase dropped files an earlier upload phase staged", host)
	}
}

// Test_57_RPMUploadFiles routes the install's RPM uploads.
func (s *ApplyPhaseSuite) Test_57_RPMUploadFiles() {
	s.rpmUploadFiles()
}

// Test_57_RPMUploadFiles routes the upgrade's RPM uploads, on the same split.
func (s *UpgradePhaseSuite) Test_57_RPMUploadFiles() {
	s.rpmUploadFiles()
}

// Test_57_RPMUploadFiles routes the join's RPM uploads. The machine being joined is a Fedora
// worker, so it is on the claimed side of the split rather than the untouched one.
func (s *JoinPhaseSuite) Test_57_RPMUploadFiles() {
	s.rpmUploadFiles()
}
