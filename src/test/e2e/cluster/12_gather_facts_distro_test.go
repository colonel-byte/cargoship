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

// Test_12_GatherFactsDistro covers phase/12_gather_facts_distro.go. It reads the engine
// version already on each node, which is what steers apply between the initialize and the
// upgrade phases. Nothing has been installed yet at this point in the order, so every host
// has to come back unknown -- that is what sends Test_61/Test_62 down the initialize path
// and leaves the upgrade phases in Test_66/Test_67 with nothing to do.
func (s *ApplyPhaseSuite) Test_12_GatherFactsDistro() {
	s.runPhase(&phase.GatherFactsDistro{Distro: s.harness.distro})

	for _, host := range s.harness.hosts() {
		s.Require().Equalf(phase.UnknownVersion, host.Metadata.DistroVersion,
			"%s: reports an engine version before anything was installed", host)
	}
}

// Test_12_GatherFactsDistro is where the upgrade walk starts to diverge from the install one.
// Every host that runs the engine now reports the version the apply walk installed, and that
// it is older than the package this walk carries is the fact the rest of the walk turns on:
// it is what keeps the initialize phases away from a running cluster in Test_61 and Test_62,
// and what hands those hosts to the upgrade phases in Test_66 and Test_67.
//
// The upload-only host still reports unknown. It never had an engine installed, so the
// version check is also the reason the upgrade phases leave it alone -- though by then it has
// been dropped from the manager anyway.
func (s *UpgradePhaseSuite) Test_12_GatherFactsDistro() {
	s.runPhase(&phase.GatherFactsDistro{Distro: s.harness.distro})

	// VersionLess is the comparison the upgrade phases' own Prepare makes. Calling it here,
	// rather than comparing the strings, means this test cannot disagree with the routing it
	// is asserting. It reads no manager state, so a zero-valued phase is enough.
	compare := &phase.GenericPhase{}
	packaged := s.harness.manager.Distro.Spec.Version

	for _, host := range s.harness.engineHosts() {
		s.Require().Equalf(installedVersion, host.Metadata.DistroVersion,
			"%s: not running the version the install walk put there", host)
		s.Require().Truef(compare.VersionLess(host, packaged),
			"%s: runs %s, which the upgrade phases do not consider older than the packaged %s",
			host, host.Metadata.DistroVersion, packaged)
	}

	for _, host := range s.harness.uploadOnly() {
		s.Require().Equalf(phase.UnknownVersion, host.Metadata.DistroVersion,
			"%s: an upload-only host reports an engine version", host)
	}
}
