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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
)

type (
	Dev  mg.Namespace
	Test mg.Namespace
)

// Clean removes build artifacts
func (Dev) Clean() error {
	return clean()
}

// Tidy just runs the module tidy
func (Dev) Tidy() error {
	fmt.Println("Running tidy")
	return sh.RunV(
		"go",
		"mod",
		"tidy",
	)
}

// Vendor just runs the module vendor
func (d Dev) Vendor() error {
	if err := d.Tidy(); err != nil {
		return err
	}

	fmt.Println("Running vendor")

	return sh.RunV(
		"go",
		"mod",
		"vendor",
	)
}

// Digest simple returns the digest of an image, mostly for testing
func (Dev) Digest(ctx context.Context) error {
	store, err := credentials.NewStoreFromDocker(credentials.StoreOptions{
		DetectDefaultNativeStore: true,
	})
	if err != nil {
		return err
	}

	repo, err := remote.NewRepository("docker.io/library/alpine")
	if err != nil {
		return err
	}

	repo.Client = &auth.Client{
		Credential: auth.CredentialFunc(func(ctx context.Context, host string) (auth.Credential, error) {
			return store.Get(ctx, host)
		}),
	}

	desc, err := repo.Resolve(ctx, "latest")
	if err != nil {
		return err
	}

	fmt.Print(desc.Digest)

	return nil
}

// EndToEnd runs the go testing the e2e suite
func (Test) EndToEnd() error {
	if err := stopBootlooseContainers(); err != nil {
		return err
	}
	if err := daggerBuildLocal(runtime.GOOS, runtime.GOARCH); err != nil {
		return err
	}
	e2eTmpDir, err := filepath.Abs(filepath.Join(buildDir, "tmp"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(e2eTmpDir, 0o755); err != nil {
		return err
	}
	return sh.RunWithV(
		map[string]string{
			"CARGOSHIP_E2E_TMPDIR": e2eTmpDir,
			"TMPDIR":               e2eTmpDir,
		},
		"go",
		"test",
		"-timeout=1h",
		"github.com/colonel-byte/cargoship/src/test/e2e",
		"-count=1",
		"-v",
	)
}

// EndToEndFast runs the parts of the e2e suite that need neither a cluster nor the large
// example packages -- the misc and package command groups. Mirrors the e2e-noncluster CI job.
func (Test) EndToEndFast() error {
	if err := daggerBuildLocal(runtime.GOOS, runtime.GOARCH); err != nil {
		return err
	}
	e2eTmpDir, err := filepath.Abs(filepath.Join(buildDir, "tmp"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(e2eTmpDir, 0o755); err != nil {
		return err
	}
	return sh.RunWithV(
		map[string]string{
			"CARGOSHIP_E2E_TMPDIR": e2eTmpDir,
			"TMPDIR":               e2eTmpDir,
		},
		"go",
		"test",
		"-timeout=30m",
		"github.com/colonel-byte/cargoship/src/test/e2e",
		"-count=1",
		"-v",
		"-short",
		"-skip", "TestClusterLifecycle",
	)
}

// stopBootlooseContainers force-removes any leftover bootloose-managed containers (e.g. from
// a previous e2e run that was killed before cluster teardown ran), so TestMain's bootloose
// Create() isn't confused by stale/exited containers with the same names.
func stopBootlooseContainers() error {
	ids, err := sh.Output("docker", "ps", "-aq", "--filter", "label=io.k0sproject.bootloose.owner=bootloose")
	if err != nil {
		return err
	}
	ids = strings.TrimSpace(ids)
	if ids == "" {
		return nil
	}
	fmt.Println("Removing leftover bootloose containers")
	return sh.RunV("docker", append([]string{"rm", "-f"}, strings.Fields(ids)...)...)
}
