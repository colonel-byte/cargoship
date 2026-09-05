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

	"github.com/colonel-byte/cargoship/src/pkg/phase"
)

// hostsFilePath is the file phase/25_modify_hosts_file.go rewrites.
const hostsFilePath = "/etc/hosts"

// hostsFileComment is the marker the phase attaches to every entry it adds, so an operator
// can tell cargoship's lines from the ones the image shipped with.
const hostsFileComment = "added by cargoship"

// Test_25_ModifyHosts covers phase/25_modify_hosts_file.go. The phase makes every node
// resolve every other node by name, so the assertion is the full matrix: each host's
// /etc/hosts carries an entry for each host in the cluster, tagged with cargoship's comment,
// and the name actually resolves on the node.
func (s *ApplyPhaseSuite) Test_25_ModifyHosts() {
	p := &phase.ModifyHosts{Enabled: s.harness.opts.ModifyHosts}
	s.runPhase(p)
	s.Require().Equal(s.harness.opts.ModifyHosts, ran(p), "phase did not follow its enabled flag")

	if !ran(p) {
		return
	}

	rendered, err := readOnHosts(s.harness.hosts(), hostsFilePath)
	s.Require().NoError(err)

	for _, host := range s.harness.hosts() {
		content := rendered[host.String()]

		for _, peer := range s.harness.hosts() {
			long := peer.Configurer.LongHostname(peer)
			short := peer.Configurer.Hostname(peer)

			s.Require().Containsf(content, peer.PrivateAddress,
				"%s: %s has no entry for %s", host, hostsFilePath, peer)
			s.Require().Containsf(content, long, "%s: %s is missing %s", host, hostsFilePath, long)
			s.Require().Containsf(content, short, "%s: %s is missing %s", host, hostsFilePath, short)
		}

		s.Require().Containsf(content, hostsFileComment,
			"%s: entries are not marked with %q", host, hostsFileComment)

		// The file is only useful if the resolver agrees with it.
		for _, peer := range s.harness.hosts() {
			out, err := host.ExecOutput(fmt.Sprintf("getent hosts %s", peer.Configurer.Hostname(peer)))
			s.Require().NoErrorf(err, "%s: cannot resolve %s", host, peer)
			s.Require().Containsf(out, peer.PrivateAddress,
				"%s: resolved %s to something other than %s", host, peer, peer.PrivateAddress)
		}
	}
}
