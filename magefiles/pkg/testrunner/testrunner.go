// Copyright 2026 colonel-byte
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package testrunner implements runners for end-to-end and fuzz test suites.
package testrunner

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/colonel-byte/cargoship/magefiles/pkg/build"
	"github.com/magefile/mage/sh"
)

// RunE2E builds the binary the suite drives, then runs go test against pkg.
func RunE2E(timeout string, pkg string, extra ...string) error {
	if err := build.Binary(runtime.GOOS, runtime.GOARCH); err != nil {
		return err
	}
	return RunE2ENoBuild(timeout, pkg, extra...)
}

// RunE2ENoBuild runs go test against pkg with the temporary directory configured for e2e suites.
func RunE2ENoBuild(timeout string, pkg string, extra ...string) error {
	e2eTmpDir, err := filepath.Abs(filepath.Join(build.BuildDir, "tmp"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(e2eTmpDir, 0o755); err != nil {
		return err
	}
	args := append([]string{"test", "-timeout=" + timeout, pkg, "-count=1", "-v"}, extra...)
	return sh.RunWithV(
		map[string]string{
			"CARGOSHIP_E2E_TMPDIR": e2eTmpDir,
			"TMPDIR":               e2eTmpDir,
		},
		"go",
		args...,
	)
}

// StopBootlooseContainers force-removes leftover bootloose containers.
func StopBootlooseContainers() error {
	ids, err := sh.Output("docker", "ps", "-aq", "--filter", "label=io.k0sproject.bootloose.owner=bootloose")
	if err != nil {
		return err
	}
	ids = strings.TrimSpace(ids)
	if ids == "" {
		return nil
	}
	fmt.Println("Removing leftover bootloose containers")
	return sh.RunV("docker", append([]string{"rm", "-fv"}, strings.Fields(ids)...)...)
}

// Unit runs go test over every package except the e2e suites under test/, which need Docker
// and runners of their own. Those are driven by Test.EndToEnd* instead.
func Unit() error {
	out, err := sh.Output("go", "list", "./...")
	if err != nil {
		return err
	}

	const e2ePrefix = "github.com/colonel-byte/cargoship/test/"
	var pkgs []string
	for _, pkg := range strings.Fields(out) {
		if strings.HasPrefix(pkg, e2ePrefix) {
			continue
		}
		pkgs = append(pkgs, pkg)
	}
	if len(pkgs) == 0 {
		return fmt.Errorf("go list ./... returned no packages to test")
	}

	return sh.RunV("go", append([]string{"test", "-count=1"}, pkgs...)...)
}

// Fuzz replays the fuzz seed corpus under fuzz/...
func Fuzz() error {
	return sh.RunV("go", "test", "-count=1", "./fuzz/...")
}
