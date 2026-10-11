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

	"github.com/colonel-byte/cargoship/magefiles/pkg/devtools/dnfpins"
	"github.com/colonel-byte/cargoship/magefiles/pkg/devtools/microvmrun"
	"github.com/colonel-byte/cargoship/magefiles/pkg/devtools/osv"
	"github.com/colonel-byte/cargoship/magefiles/pkg/util"
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
	return util.CleanBuild()
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
	pins, err := dnfpins.QueryLatestAlmaLinuxPackages(ctx)
	if err != nil {
		return fmt.Errorf("querying AlmaLinux packages: %w", err)
	}

	fmt.Printf("Discovered versions:\n  ansible-core:    %s\n  bash-completion: %s\n",
		pins.AnsibleCore, pins.BashCompletion)

	if err := dnfpins.UpdateDockerfileAnsiblePins(dnfpins.AnsibleDockerfilePath, pins.AnsibleCore, pins.BashCompletion); err != nil {
		return fmt.Errorf("updating %s: %w", dnfpins.AnsibleDockerfilePath, err)
	}
	fmt.Printf("Updated %s\n", dnfpins.AnsibleDockerfilePath)

	if err := dnfpins.UpdateDockerfileUbiPins(dnfpins.UbiDockerfilePath, pins.BashCompletion); err != nil {
		return fmt.Errorf("updating %s: %w", dnfpins.UbiDockerfilePath, err)
	}
	fmt.Printf("Updated %s\n", dnfpins.UbiDockerfilePath)

	if err := dnfpins.UpdateGoreleaserDnfPins(dnfpins.GoreleaserConfigPath, pins); err != nil {
		return fmt.Errorf("updating %s: %w", dnfpins.GoreleaserConfigPath, err)
	}
	fmt.Printf("Updated %s\n", dnfpins.GoreleaserConfigPath)

	return nil
}

// VMImage fetches and verifies the base image the VM fleet runs, without starting anything.
func (Dev) VMImage(ctx context.Context) error {
	return microvmrun.Image(ctx)
}

// VMUp brings up a local Enterprise Linux VM fleet and writes a ZarfCluster inventory pointing
// at it, so that the phases a container cannot exercise - SELinux, fapolicyd, firewalld, the
// *-selinux RPM scriptlets - can be run against a real kernel. See docs/dev/microvm.md.
//
// Infra nodes join as workers; what makes them infra is a profile of their own, which the
// LabelNodes phase turns into a node-role.kubernetes.io/infra label.
func (Dev) VMUp(
	ctx context.Context,
	// control-plane nodes (default 1)
	control *int,
	// worker nodes (default 2)
	worker *int,
	// infra nodes, which join as workers under an infra profile (default 0)
	infra *int,
	// engine the fleet is prepared for, k3s or rke2 (default k3s)
	distro *string,
) error {
	return microvmrun.Up(ctx, microvmrun.Options{
		Controllers: intOr(control, 1),
		Workers:     intOr(worker, 2),
		Infra:       intOr(infra, 0),
		Distro:      stringOr(distro, "k3s"),
	})
}

// VMDown tears a VM fleet down and removes its state, leaving the cached base image.
//
// The fleet name matters when the e2e suite has been run: CARGOSHIP_E2E_BACKEND=microvm brings
// up a fleet called "e2e", which dev:vmUp's "dev" fleet does not cover. A run killed before
// its teardown leaves that one behind, and `-fleet=e2e` is how to remove it.
func (Dev) VMDown(
	// fleet to tear down (default dev)
	fleet *string,
) error {
	return microvmrun.Down(stringOr(fleet, ""))
}

// VMList prints a VM fleet's nodes and whether each is running.
func (Dev) VMList(
	// fleet to list (default dev)
	fleet *string,
) error {
	return microvmrun.List(stringOr(fleet, ""))
}

// VMShell opens a shell on one node of a VM fleet: `mage dev:vmShell kc0`.
func (Dev) VMShell(
	ctx context.Context,
	// node to connect to
	node string,
	// fleet the node belongs to (default dev)
	fleet *string,
) error {
	return microvmrun.SSH(ctx, stringOr(fleet, ""), node)
}

// WriteOSVOverrides writes every override into vendor/.
func (Dev) WriteOSVOverrides() error {
	return osv.WriteOverrides()
}

// VerifyVendor checks that vendor/ contains all required overrides and no uncovered manifests.
func (Dev) VerifyVendor() error {
	return osv.VerifyVendor()
}

// intOr is the value of an optional mage argument, or def when it was not given. Mage passes
// an unset optional argument as a nil pointer.
func intOr(v *int, def int) int {
	if v == nil {
		return def
	}
	return *v
}

// stringOr is the string equivalent. An argument given as -distro= with nothing after it
// arrives as an empty string rather than nil, which is a typo and not a request for the
// default, so it is left to microvm.Spec.Normalize to reject.
func stringOr(v *string, def string) string {
	if v == nil {
		return def
	}
	return *v
}
