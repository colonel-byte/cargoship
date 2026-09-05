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

// Test_91_Lock covers phase/91_lock.go, which apply runs third, right after the OS is
// detected, so that it holds the cluster for the whole run. Here it runs in its file-number
// position instead, after the install, and phase/92_unlock.go releases it in Test_92. What
// that costs is stated in the ApplyPhaseSuite doc comment; what it tests is unchanged.
//
// The lock is a file on each host holding the instance ID of the process that took it, so
// the assertion is that the file exists on every host and names this test binary.
func (s *ApplyPhaseSuite) Test_91_Lock() {
	s.runPhase(s.harness.lock)

	want, err := lockFileContent()
	s.Require().NoError(err)

	for _, host := range s.harness.hosts() {
		path := host.Configurer.CTLLockFilePath(host)
		s.Require().Truef(host.FileExist(path), "%s: no lock file at %s", host, path)

		got, err := host.Configurer.ReadFile(host, path)
		s.Require().NoError(err)
		s.Require().Equalf(want, got, "%s: lock file is not held by this test binary", host)
	}
}
