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
	"github.com/colonel-byte/cargoship/pkg/phase"
)

// osIDByPrefix maps a bootloose name template, with the index placeholder removed, to the
// os-release ID the image behind it reports. The prefixes are derived from the templates
// rather than written out again so that renaming a replica group in main_test.go cannot
// silently stop this check from matching.
//
// The Ubuntu prefixes are prefixes of the others ("kw" of both "kwf" and "kwa"), so the
// lookup has to be longest-match rather than first-match.

// detectOS covers phase/09_detect_os.go. It resolves the per-host Configurer that
// every phase after it calls through, so the assertion is that each host now has one and
// that it reports the OS its image actually runs.
//
// This is also where the cluster's OS mix is checked. Several later phases route on the OS
// family and assert that the routing matches what the hosts report; if the cluster quietly
// lost a family those assertions would still pass, having tested nothing. Failing here
// instead points at the cause rather than at a phase that no longer covers a branch.
//
// Both walks that run this phase assert the same thing, so the body is shared: see phaseWalk.
func (s *phaseWalk) detectOS() {
	s.T().Helper()

	s.runPhase(&phase.DetectOS{})

	families := make(map[string]int, 2)
	for _, host := range s.harness.hosts() {
		s.Require().NotNilf(host.Configurer, "%s: configurer not resolved", host)

		kind, err := host.OSKind()
		s.Require().NoError(err)
		s.Require().Equal("linux", kind)

		s.Require().NotEmptyf(host.Configurer.Hostname(host), "%s: configurer resolved no hostname", host)

		release, err := host.OS()
		s.Require().NoErrorf(err, "%s: failed to read the OS release", host)
		s.Require().NotEmptyf(release.ID, "%s: the configurer resolved no os-release ID", host)

		// A fleet that declares what each group runs is held to it. An externally supplied
		// inventory declares nothing -- the operating systems are whatever the operator's
		// hosts run, and are not knowable until this phase has asked them -- so there the
		// assertion is only that detection produced something.
		if want := clusterConfig().osIDFor(host.Hostname); want != "" {
			s.Require().Equalf(want, release.ID,
				"%s: detected a different OS than the image the machine was built from", host)
		}
		families[release.ID]++
	}

	if declared := clusterConfig().osIDs(); len(declared) > 0 {
		s.Require().Lenf(families, len(declared),
			"the cluster has to run every OS family for the family-routed phases to be tested, it runs %v", families)
	} else {
		s.T().Logf("the fleet declares no operating systems; it runs %v", families)
	}
}

func (s *ApplyPhaseSuite) Test_09_DetectOS() {
	s.detectOS()
}

// Test_09_DetectOS resolves the new node's configurer, and re-checks that the cluster still
// runs every OS family now that a machine has been added to it.
func (s *JoinPhaseSuite) Test_09_DetectOS() {
	s.detectOS()
}
