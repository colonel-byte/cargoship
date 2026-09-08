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

//go:build mage
// +build mage

package main

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

type (
	Test mg.Namespace
)

// runE2E builds the binary the suite drives, then runs go test against pkg. Use it for the
// suites that shell out to the CLI; a suite that calls the packages directly wants
// runE2ENoBuild instead.
func runE2E(timeout string, pkg string, extra ...string) error {
	if err := daggerBuildLocal(runtime.GOOS, runtime.GOARCH); err != nil {
		return err
	}
	return runE2ENoBuild(timeout, pkg, extra...)
}

// runE2ENoBuild runs go test against pkg with the temp directory the e2e suites write into,
// and builds nothing.
func runE2ENoBuild(timeout string, pkg string, extra ...string) error {
	e2eTmpDir, err := filepath.Abs(filepath.Join(buildDir, "tmp"))
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
