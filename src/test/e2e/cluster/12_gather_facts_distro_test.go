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

// Test_12_GatherFactsDistro is where the join walk diverges from both the install and the
// upgrade. The nodes the install bootstrapped report the version it put there, and the machine
// being joined reports none, because nothing has ever been installed on it. That one host
// differs from the rest is what hands it, and only it, to the initialize phase in Test_62.
//
// The version the established nodes report is the version this walk's package carries, so
// nothing here is older than the package and the upgrade phases stay out of the join.
func (s *JoinPhaseSuite) Test_12_GatherFactsDistro() {
	s.runPhase(&phase.GatherFactsDistro{Distro: s.harness.distro})

	for _, host := range s.harness.engineHosts() {
		if s.isJoined(host) {
			s.Require().Equalf(phase.UnknownVersion, host.Metadata.DistroVersion,
				"%s: the machine being joined reports an engine version before anything installed one", host)
			continue
		}
		s.Require().Equalf(installedVersion, host.Metadata.DistroVersion,
			"%s: not running the version the install walk put there", host)
	}

	for _, host := range s.harness.uploadOnly() {
		s.Require().Equalf(phase.UnknownVersion, host.Metadata.DistroVersion,
			"%s: an upload-only host reports an engine version", host)
	}
}
