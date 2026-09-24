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

//go:build mage
// +build mage

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/magefile/mage/sh"
)

// Examples builds a package from every example definition, with the cargoship on PATH.
//
// It uses the installed CLI rather than a binary built here, so what it exercises is what a
// user gets from a release, the same as .github/workflows/publish-example.yaml does for a
// single flavor. Run mage build:binary and put build/ on PATH first to test a local build
// instead.
//
// Every example is built, which is gigabytes per package and hours in total: it pulls each
// definition's whole image list and its RPMs or binaries. One failure does not stop the run,
// since a broken definition should not throw away the packages that already built; the
// failures are reported together at the end.
//
// Packages are written to build/examples, and TMPDIR is pointed at build/tmp because a
// package is assembled in temporary space before it is written out, which is more than a
// tmpfs /tmp usually has.
func (Build) Examples() error {
	bin, err := exec.LookPath("cargoship")
	if err != nil {
		return fmt.Errorf("cargoship must be on PATH: %w", err)
	}

	dirs, err := exampleDefinitions()
	if err != nil {
		return err
	}
	if len(dirs) == 0 {
		return errors.New("no example definitions found under example/")
	}

	out, err := filepath.Abs(filepath.Join(buildDir, "examples"))
	if err != nil {
		return err
	}
	tmp, err := filepath.Abs(filepath.Join(buildDir, "tmp"))
	if err != nil {
		return err
	}
	for _, dir := range []string{out, tmp} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	signingKey := os.Getenv("SIGNING_KEY")
	signingKeyPass := os.Getenv("SIGNING_KEY_PASS")
	reproducible := os.Getenv("REPRODUCIBLE") == "true" || os.Getenv("REPRODUCIBLE") == "1"

	var extraFlags []string
	if signingKey != "" {
		extraFlags = append(extraFlags, "--signing-key", signingKey)
	}
	if signingKeyPass != "" {
		extraFlags = append(extraFlags, "--signing-key-pass", signingKeyPass)
	}
	if reproducible {
		extraFlags = append(extraFlags, "--reproducible")
	}

	fmt.Printf("Building %d examples with %s into %s\n", len(dirs), bin, out)

	var failed []error
	for i, dir := range dirs {
		fmt.Printf("\n[%d/%d] %s\n", i+1, len(dirs), dir)

		args := append([]string{"create", dir, "--output", out, "--confirm"}, extraFlags...)
		start := time.Now()
		err := sh.RunWithV(
			map[string]string{"TMPDIR": tmp},
			bin,
			args...,
		)
		if err != nil {
			fmt.Printf("FAILED %s: %v\n", dir, err)
			failed = append(failed, fmt.Errorf("building %s: %w", dir, err))
			continue
		}
		fmt.Printf("Built %s in %s\n", dir, time.Since(start).Round(time.Second))
	}

	fmt.Printf("\n%d of %d examples built, %d failed\n", len(dirs)-len(failed), len(dirs), len(failed))
	return errors.Join(failed...)
}

// exampleDefinitions returns every directory under example/ holding a distro.yaml, sorted so
// a run covers the flavors in a stable order. Examples live at
// example/<flavor>/<minor>/<version>/distro.yaml, so the glob is fixed at that depth rather
// than walking the tree, which keeps a stray distro.yaml elsewhere under example/ out of it.
func exampleDefinitions() ([]string, error) {
	matches, err := filepath.Glob(filepath.Join("example", "*", "*", "*", "distro.yaml"))
	if err != nil {
		return nil, err
	}

	dirs := make([]string, 0, len(matches))
	for _, m := range matches {
		dirs = append(dirs, filepath.Dir(m))
	}
	sort.Strings(dirs)
	return dirs, nil
}
