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

// osIDByPrefix maps a bootloose name template, with the index placeholder removed, to the
// os-release ID the image behind it reports. The prefixes are derived from the templates
// rather than written out again so that renaming a replica group in main_test.go cannot
// silently stop this check from matching.
//
// The Ubuntu prefixes are prefixes of the others ("kw" of both "kwf" and "kwa"), so the
// lookup has to be longest-match rather than first-match.
var osIDByPrefix = map[string]string{ //nolint:gochecknoglobals
	strings.TrimSuffix(bootKC, "%d"):  "ubuntu",
	strings.TrimSuffix(bootKW, "%d"):  "ubuntu",
	strings.TrimSuffix(bootKCF, "%d"): "fedora",
	strings.TrimSuffix(bootKWF, "%d"): "fedora",
	strings.TrimSuffix(bootKWA, "%d"): "alpine",
}

// expectedOSID returns the os-release ID the image behind a machine name reports, or an empty
// string for a name no replica group produces.
func expectedOSID(hostname string) string {
	id := ""
	longest := 0
	for prefix, osID := range osIDByPrefix {
		if strings.HasPrefix(hostname, prefix) && len(prefix) > longest {
			id, longest = osID, len(prefix)
		}
	}
	return id
}

// Test_09_DetectOS covers phase/09_detect_os.go. It resolves the per-host Configurer that
// every phase after it calls through, so the assertion is that each host now has one and
// that it reports the OS its image actually runs.
//
// This is also where the cluster's OS mix is checked. Several later phases route on the OS
// family and assert that the routing matches what the hosts report; if the cluster quietly
// lost a family those assertions would still pass, having tested nothing. Failing here
// instead points at the cause rather than at a phase that no longer covers a branch.
func (s *ApplyPhaseSuite) Test_09_DetectOS() {
	s.runPhase(&phase.DetectOS{})

	families := make(map[string]int, 2)
	for _, host := range s.harness.hosts() {
		s.Require().NotNilf(host.Configurer, "%s: configurer not resolved", host)

		kind, err := host.OSKind()
		s.Require().NoError(err)
		s.Require().Equal("linux", kind)

		s.Require().NotEmptyf(host.Configurer.Hostname(host), "%s: configurer resolved no hostname", host)

		s.Require().Equalf(expectedOSID(host.Hostname), host.OSVersion.ID,
			"%s: detected a different OS than the image the machine was built from", host)
		families[host.OSVersion.ID]++
	}

	s.Require().Lenf(families, len(uniqueOSIDs()),
		"the cluster has to run every OS family for the family-routed phases to be tested, it runs %v", families)
}

// uniqueOSIDs returns the distinct os-release IDs the bootloose config provisions.
func uniqueOSIDs() map[string]struct{} {
	ids := make(map[string]struct{}, len(osIDByPrefix))
	for _, id := range osIDByPrefix {
		ids[id] = struct{}{}
	}
	return ids
}
