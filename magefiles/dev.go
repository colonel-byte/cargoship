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

	"github.com/colonel-byte/cargoship/magefiles/pkg/dnfpins"
	"github.com/colonel-byte/cargoship/magefiles/pkg/hostbuild"
	"github.com/colonel-byte/cargoship/magefiles/pkg/osv"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

type (
	Dev mg.Namespace
)

// Clean removes build artifacts
func (Dev) Clean() error {
	return hostbuild.Clean()
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
// just recreated. See magefiles/pkg/osv/osv.go for what those are and why they cannot live
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

// DnfPins queries the AlmaLinux 10 RPM repodata over HTTP and updates the pinned
// versions of packages installed via dnf/microdnf in containers/ubi/Dockerfile,
// containers/ansible/Dockerfile, and .goreleaser.yaml.
func (Dev) DnfPins(ctx context.Context) error {
	return dnfpins.Update(ctx)
}

// WriteOSVOverrides writes the generated osv-scanner.toml files into vendor/
//
// go mod vendor deletes and recreates the tree, so this runs after it every time rather than
// relying on the files surviving. See docs/agent/choice-osv-vendor-overrides.md.
func (Dev) WriteOSVOverrides() error {
	return osv.Write()
}

// VerifyVendor checks that vendor/ still says what Dev.Vendor wrote
//
// Every declared osv-scanner.toml present and byte-identical, and no foreign dependency manifest
// that nothing covers. The second half turns a silent Scorecard regression into a failure on the
// commit that caused it.
func (Dev) VerifyVendor() error {
	return osv.Verify()
}
