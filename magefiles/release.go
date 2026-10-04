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
	"github.com/colonel-byte/cargoship/magefiles/pkg/release"
	"github.com/magefile/mage/mg"
)

// Release holds the release artifacts goreleaser does not build. See magefiles/pkg/release.
type Release mg.Namespace

// TofuProvider builds the OpenTofu provider for every published platform and pushes it to an OCI
// registry as a provider mirror artifact.
func (Release) TofuProvider(
	// release version, without the leading v (e.g. 0.1.0)
	version string,
	// OCI repository to publish to (defaults to the ghcr.io provider mirror)
	repository *string,
) error {
	opts := release.Options{
		Version: version,
		Push:    true,
	}
	if repository != nil {
		opts.Repository = *repository
	}
	return release.TofuProvider(opts)
}

// TofuProviderLayout builds the same artifact and stops before pushing it, leaving the OCI layout
// under build/ so the shape can be checked without a registry.
func (Release) TofuProviderLayout(
	// release version, without the leading v (e.g. 0.1.0)
	version string,
) error {
	return release.TofuProvider(release.Options{Version: version})
}
