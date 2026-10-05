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

package zarfmod

import "os"

// stdoutGuard holds the real standard output of a module run.
//
// An Ansible module must write exactly one JSON object on stdout and nothing else. The wrapper
// duplicates file descriptor 1, points os.Stdout at stderr, and writes its one object to the
// duplicate. Everything that reaches for os.Stdout afterwards reaches stderr, which Ansible
// captures as module output and shows on failure.
//
// This matters more here than it would inside zarf itself. The wrapper runs zarf as a child
// process, and zarf's progress rendering writes to its own stdout, so the child's stdout is wired
// to the wrapper's stderr rather than inherited. The guard is what makes that safe to get wrong:
// anything that still reaches fd 1 by another route lands on stderr too.
type stdoutGuard struct {
	out *os.File
	// restore is what os.Stdout held before the guard took over. The process exits straight after
	// a module run and never needs it back, but a test does, and a guard that cannot be undone is
	// a guard that cannot be tested.
	restore *os.File
}

// Write sends bytes to the standard output the module was started with.
func (g *stdoutGuard) Write(b []byte) (int, error) {
	return g.out.Write(b)
}

// Close puts os.Stdout back and releases the duplicated descriptor.
func (g *stdoutGuard) Close() error {
	os.Stdout = g.restore
	return g.out.Close()
}
