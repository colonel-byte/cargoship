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

// Package repo locates the repository root for the tests under magefiles/pkg.
//
// Every mage target runs with the repository root as its working directory, so the generators
// under magefiles/pkg address their inputs and outputs by paths relative to it -- "docs/SUMMARY.md",
// "ansible/colonel_byte/cargoship", "magefiles/templates". A test binary instead runs in the
// directory of the package under test, several levels down, where none of those paths resolve.
//
// The root is found by walking up from this file until a go.mod appears, rather than by counting
// "../" from the test's own directory. AGENTS.md calls out hand-encoded depth as something that
// breaks silently when either endpoint moves and that go vet cannot catch; a package moved one
// level deeper here needs no edit at all.
package repo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Root returns the absolute path of the repository root.
func Root() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("unable to locate the repository root: no caller information")
	}

	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("unable to locate the repository root: no go.mod above %s", filepath.Dir(file))
		}
		dir = parent
	}
}

// Chdir points the test at the repository root for the rest of its run, and restores the working
// directory afterwards. Use it in any test that reaches a checked-in file the way a target does.
//
// The working directory is process-wide, so a test calling this cannot run in parallel with one
// that reads a relative path of its own.
func Chdir(tb testing.TB) string {
	tb.Helper()

	root, err := Root()
	if err != nil {
		tb.Fatalf("locating the repository root: %v", err)
	}

	previous, err := os.Getwd()
	if err != nil {
		tb.Fatalf("reading the working directory: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		tb.Fatalf("changing to %s: %v", root, err)
	}
	tb.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			tb.Errorf("restoring the working directory to %s: %v", previous, err)
		}
	})

	return root
}
