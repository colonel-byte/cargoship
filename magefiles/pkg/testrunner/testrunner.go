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
	"github.com/colonel-byte/cargoship/magefiles/pkg/release"
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

// RunE2EZarf builds the zarf Ansible module files, then runs the zarf module e2e suite.
//
// The timeout is generous because the suite stands up two k3d clusters and walks four zarf
// packages across each of them; the first run also pulls the packages.
func RunE2EZarf() error {
	if err := build.ZarfModuleFiles(); err != nil {
		return err
	}
	return RunE2ENoBuild("90m", "github.com/colonel-byte/cargoship/test/e2e/zarf/...")
}

// TofuProviderVersion is the version the tofu provider suite builds into the local mirror and
// the module's `>=0.1.0` constraint resolves to. OpenTofu refuses 0.0.0, which is reserved for a
// provider that is not published, so the development loop and the suite both use 0.1.0. It is
// also spelled in test/e2e/tofu/main_test.go, which is what skips the suite when the mirror does
// not hold it.
const TofuProviderVersion = "0.1.0"

// RunE2ETofu builds the provider into the filesystem mirror the suite reads, then runs the tofu
// provider e2e suite.
//
// The mirror is built here rather than by the suite because building it is a release step, not a
// test one: the same function the development loop calls, producing the same layout a `.tofurc`
// points at. A suite run without it skips rather than fails, naming the target.
//
// The timeout is generous because the suite stands up three bootloose machines, builds a distro
// package, installs an engine across them, removes a node and destroys the cluster -- each of
// those a real apply through the provider's own subprocess.
func RunE2ETofu() error {
	mirror, err := release.TofuProviderHost(TofuProviderVersion)
	if err != nil {
		return err
	}
	fmt.Printf("the suite will resolve the provider from %s\n", mirror)
	return RunE2ENoBuild("90m", "github.com/colonel-byte/cargoship/test/e2e/tofu/...")
}

// K3dClusters are the clusters the zarf module e2e suite creates, named in
// test/e2e/zarf/testdata/k3d-config.yaml and k3d-config-local-storage.yaml.
var K3dClusters = []string{"zarf", "zarf-local-storage"} //nolint:gochecknoglobals

// DeleteK3dClusters removes the clusters the zarf module suite creates, for a run that was
// interrupted before its own cleanup ran. A cluster that is not there is not an error.
func DeleteK3dClusters() error {
	for _, name := range K3dClusters {
		out, err := sh.Output("k3d", "cluster", "list", name, "--no-headers")
		if err != nil || strings.TrimSpace(out) == "" {
			continue
		}
		fmt.Printf("Deleting the k3d cluster %s\n", name)
		if err := sh.RunV("k3d", "cluster", "delete", name); err != nil {
			return err
		}
	}
	return nil
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
