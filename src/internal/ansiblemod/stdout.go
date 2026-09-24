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

package ansiblemod

import "os"

// stdoutGuard holds the real standard output of a module run.
//
// An Ansible module must write exactly one JSON object on stdout and nothing else. Cargoship's
// logging already goes to stderr, but relying on that is relying on an audit staying true: any
// fmt.Println added later, anywhere in the binary, silently breaks every playbook using the module
// and the failure reads as a JSON parse error with no obvious cause.
//
// So the module does not audit its writers. It duplicates file descriptor 1, points os.Stdout at
// stderr, and writes its one object to the duplicate. Everything that reaches for os.Stdout
// afterwards reaches stderr, which Ansible captures as module output and shows on failure.
//
// The one window this does not cover is package initialisation, which runs before main and so
// before any of this. src/cmd/root.go builds the root command in a package-level variable, and the
// two configuration errors it can report from there write to stderr for exactly this reason.
type stdoutGuard struct {
	out *os.File
	// restore is what os.Stdout held before the guard took over. The binary exits straight
	// after a module run and never needs it back, but a test does, and a guard that cannot be
	// undone is a guard that cannot be tested.
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
