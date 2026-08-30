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

// EndToEnd runs the whole e2e suite, including the example packages that pull ~1.5GB of
// engine artifacts and images.
func (Test) EndToEnd() error {
	return runE2E("1h", "github.com/colonel-byte/cargoship/src/test/e2e/...")
}

// EndToEndNonCluster runs the group that needs no cluster: the misc and package command
// groups. -short additionally skips the example packages, so this finishes in seconds.
// Mirrors the e2e-noncluster CI job.
func (Test) EndToEndNonCluster() error {
	return runE2E("30m", "github.com/colonel-byte/cargoship/src/test/e2e/noncluster/...", "-short")
}

// runE2E builds the binary the e2e suites drive, then runs go test against pkg with the
// temp directory both the suites and the binary under test write into.
func runE2E(timeout string, pkg string, extra ...string) error {
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
