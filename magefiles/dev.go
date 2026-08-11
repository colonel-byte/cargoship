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
	return sh.RunV(
		"go",
		"mod",
		"tidy",
	)
}

// Vendor just runs the module vendor
func (Dev) Vendor() error {
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
	if err := daggerBuildLocal(runtime.GOOS, runtime.GOARCH); err != nil {
		return err
	}
	return sh.RunV(
		"go",
		"test",
		"-timeout=1h",
		"github.com/colonel-byte/cargoship/src/test/e2e",
		"-count=1",
		"-v",
	)
}
