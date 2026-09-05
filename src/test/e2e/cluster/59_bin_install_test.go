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

// Test_59_BINUploadFiles covers phase/59_bin_install.go, the catch-all upload phase for a
// distro that ships neither RPMs nor debs -- which is how rke2 installs. It stages the
// engine and hands each host the install hook the initialize phases later call, so the
// assertion is that every host came out of it with a hook and with its staged files present.
//
// This is the last phase the upload-only hosts take part in, and the reason they are in the
// inventory: an Alpine host belongs to neither the Enterprise Linux nor the Debian family, so
// it is the one host this phase claims as the path it is rather than as a fallback for a host
// the RPM and APT phases declined. The next phase drops those hosts.
func (s *ApplyPhaseSuite) Test_59_BINUploadFiles() {
	s.runPhase(&phase.BINUploadFiles{Distro: s.harness.distro})

	uploadOnly := s.harness.uploadOnly()
	s.Require().NotEmpty(uploadOnly,
		"the inventory has no upload-only host, so this phase is only being tested as a fallback")

	for _, host := range uploadOnly {
		s.Require().NotNilf(host.Metadata.Install,
			"%s: the upload-only host got no install hook, so this phase declined it", host)
		s.Require().NotEmptyf(s.manifestOn(host),
			"%s: the upload-only host received no files", host)
	}

	for _, host := range s.harness.hosts() {
		s.Require().NotNilf(host.Metadata.Install,
			"%s: no install hook for the initialize phases to call", host)

		entries := s.manifestOn(host)
		s.Require().NotEmptyf(entries, "%s: upload manifest is empty", host)
		for _, path := range entries {
			s.Require().Truef(host.FileExist(path),
				"%s: manifest claims %s but it is not on the host", host, path)
		}
	}
}
