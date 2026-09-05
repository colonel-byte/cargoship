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

// Test_21_PrepareSelinux covers phase/21_prepare_selinux.go. The phase only runs on hosts
// reporting SELinux enabled. Whether any do depends on the container runtime rather than on
// the image -- a Fedora container gets no /sys/fs/selinux of its own unless the host shares
// one -- so the assertion is that the phase agrees with what the hosts report: skipped when
// no host has SELinux, and installing container-selinux everywhere when some host does.
func (s *ApplyPhaseSuite) Test_21_PrepareSelinux() {
	var selinux int
	for _, host := range s.harness.hosts() {
		if host.Configurer.SELinuxEnabled(host) {
			selinux++
		}
	}

	p := &phase.PrepareSelinux{}
	s.runPhase(p)
	s.Require().Equalf(selinux > 0, ran(p),
		"phase ran=%v with %d SELinux hosts", ran(p), selinux)

	if selinux == 0 {
		return
	}

	for _, host := range s.harness.hosts() {
		if !host.Configurer.SELinuxEnabled(host) {
			continue
		}
		s.Require().Truef(host.Configurer.CommandExist(host, "semodule"),
			"%s: SELinux host did not get the container-selinux tooling", host)
	}
}
