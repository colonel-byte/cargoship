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

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
)

type (
	Dev mg.Namespace
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

// Vendor runs the module vendor and then writes the osv-scanner overrides back into the tree it
// just recreated. See magefiles/vendor-osv.go for what those are and why they cannot live
// anywhere else; Dev.VerifyVendor is the check that they are still there.
func (d Dev) Vendor() error {
	if err := d.Tidy(); err != nil {
		return err
	}

	fmt.Println("Running vendor")

	if err := sh.RunV(
		"go",
		"mod",
		"vendor",
	); err != nil {
		return err
	}

	return d.WriteOSVOverrides()
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

// DnfPins queries the AlmaLinux 10 RPM repodata over HTTP and updates the pinned
// versions of packages installed via dnf/microdnf in containers/ubi/Dockerfile,
// containers/ansible/Dockerfile, and .goreleaser.yaml.
func (Dev) DnfPins(ctx context.Context) error {
	fmt.Println("Querying AlmaLinux 10 repodata for latest package versions...")
	pins, err := queryLatestAlmaLinuxPackages(ctx)
	if err != nil {
		return fmt.Errorf("querying AlmaLinux packages: %w", err)
	}

	fmt.Printf("Discovered versions:\n  ansible-core:    %s\n  bash-completion: %s\n  shadow-utils:    %s\n",
		pins.AnsibleCore, pins.BashCompletion, pins.ShadowUtils)

	if err := updateDockerfileAnsiblePins(ansibleDockerfilePath, pins.AnsibleCore, pins.BashCompletion); err != nil {
		return fmt.Errorf("updating %s: %w", ansibleDockerfilePath, err)
	}
	fmt.Printf("Updated %s\n", ansibleDockerfilePath)

	if err := updateDockerfileUbiPins(ubiDockerfilePath, pins.ShadowUtils, pins.BashCompletion); err != nil {
		return fmt.Errorf("updating %s: %w", ubiDockerfilePath, err)
	}
	fmt.Printf("Updated %s\n", ubiDockerfilePath)

	if err := updateGoreleaserDnfPins(goreleaserConfigPath, pins); err != nil {
		return fmt.Errorf("updating %s: %w", goreleaserConfigPath, err)
	}
	fmt.Printf("Updated %s\n", goreleaserConfigPath)

	return nil
}
