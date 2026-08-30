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

package test

import (
	"fmt"
	"os"
	"path/filepath"

	zconfig "github.com/zarf-dev/zarf/src/config"
)

// Bootstrap chdirs into the repo root and returns a CargoE2ETest pointed at the binary
// built for this platform, along with the root it moved into. Each e2e suite calls this
// from TestMain: the suites spell every path (example distros, config files, testdata)
// relative to the repo root, and the binary under test is looked up under build/.
func Bootstrap() (CargoE2ETest, string, error) {
	root, err := RepoRoot()
	if err != nil {
		return CargoE2ETest{}, "", err
	}
	if err := os.Chdir(root); err != nil {
		return CargoE2ETest{}, "", err
	}

	e2e := CargoE2ETest{
		Arch:         zconfig.GetArch(),
		CargoBinPath: filepath.Join(root, "build", GetCLIName()),
	}
	if _, err := os.Stat(e2e.CargoBinPath); err != nil {
		return CargoE2ETest{}, "", fmt.Errorf("cargoship binary %s not found, build it first: %w", e2e.CargoBinPath, err)
	}

	return e2e, root, nil
}

// RepoRoot walks up from the working directory to the directory holding go.mod, so a
// suite can sit at any depth under src/test/e2e without hard-coding how far up the root is.
func RepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found in any parent of %s", dir)
		}
		dir = parent
	}
}
