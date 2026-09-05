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

// Test_92_Unlock covers phase/92_unlock.go, the other end of the lock Test_91 took. Apply
// builds it from the lock phase itself, and so does this suite, so the cancel it calls is
// the one that stops the tickers keeping the lock file alive. The assertion is that the file
// is gone from every host afterwards: a lock left behind delays the next run by 30 seconds
// on every node.
func (s *ApplyPhaseSuite) Test_92_Unlock() {
	s.runPhase(s.harness.lock.UnlockPhase())

	for _, host := range s.harness.hosts() {
		path := host.Configurer.CTLLockFilePath(host)
		s.Require().Falsef(host.FileExist(path), "%s: lock file still at %s", host, path)
	}
}
