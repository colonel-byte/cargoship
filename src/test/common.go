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

// Package test provides e2e tests for Cargoship
package test

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"slices"
	"testing"

	"github.com/colonel-byte/cargoship/src/pkg/utils"
	"github.com/zarf-dev/zarf/src/pkg/utils/exec"
)

// CargoE2ETest Struct holding common fields most of the tests will utilize.
type CargoE2ETest struct {
	CargoBinPath      string
	Arch              string
	ApplianceMode     bool
	ApplianceModeKeep bool
	// ClusterConfigPath is the absolute path to the generated ZarfCluster inventory YAML
	// pointing at the bootloose-provisioned test cluster. Empty if no cluster was set up.
	ClusterConfigPath string
}

// Cargoship executes a Cargoship command.
func (e2e *CargoE2ETest) Cargoship(t *testing.T, args ...string) (_ string, _ string, err error) {
	return e2e.CargoInDir(t, "", args...)
}

// CargoInDir executes a Cargoship command in specific directory.
func (e2e *CargoE2ETest) CargoInDir(t *testing.T, dir string, args ...string) (_ string, _ string, err error) {
	if !slices.Contains(args, "--no-color") {
		args = append(args, "--no-color")
	}
	if !slices.Contains(args, "--tmpdir") {
		tmpdir, err := os.MkdirTemp(os.Getenv("CARGOSHIP_E2E_TMPDIR"), utils.TmpPathPrefix)
		if err != nil {
			return "", "", err
		}
		defer func(path string) {
			errRemove := os.RemoveAll(path)
			err = errors.Join(err, errRemove)
		}(tmpdir)
		args = append(args, "--tmpdir", tmpdir)
	}
	cfg := exec.PrintCfg()
	cfg.Dir = dir
	return exec.CmdWithTesting(t, cfg, e2e.CargoBinPath, args...)
}

// GetCLIName looks at the OS and CPU architecture to determine which Cargoship binary needs to be run.
func GetCLIName() string {
	return fmt.Sprintf("cargoship_%s_%s", runtime.GOOS, runtime.GOARCH)
}
