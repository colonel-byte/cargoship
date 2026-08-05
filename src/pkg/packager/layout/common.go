// Copyright 2021 zarf authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from zarf:
// https://github.com/zarf-dev/zarf
//
// Modifications Copyright 2026 colonel-byte.
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

// Package layout is used to defining the distro package files
package layout

import (
	"errors"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/types"
	"github.com/zarf-dev/zarf/src/pkg/signing"
)

// Distro struct
type Distro struct {
	cfg    *types.DistroConfig
	distro distro.ZarfDistro
	tmp    string
}

// DistroLayout struct
type DistroLayout struct {
	dirPath string
	digest  string
	cache   *manifestCache
	Distro  distro.ZarfDistro
}

// DistroLayoutOptions struct
type DistroLayoutOptions struct {
	VerifyBlobOptions *signing.VerifyBlobOptions
	// VerificationStrategy specifies whether verification is enforced
	VerificationStrategy VerificationStrategy
}

// VerificationStrategy describes a strategy for determining whether to verify a package.
type VerificationStrategy int

const (
	// VerifyIfPossible will attempt a verification, it will not error if verification
	// data is missing. But it will not stop processing if verification fails.
	VerifyIfPossible VerificationStrategy = iota
	// VerifyAlways will always attempt a verification, and will fail if the
	// verification fails.
	VerifyAlways
	// VerifyNever will skip all verification of a package.
	VerifyNever
)

// ErrNoVerificationMaterial is returned when there is nothing to verify against.
// VerifyIfPossible tolerates this; all other verification errors are always fatal.
var ErrNoVerificationMaterial = errors.New("no verification material available")

// New creates a new Distro object
func New(cfg *types.DistroConfig) (*Distro, error) {
	dis := Distro{
		cfg:    cfg,
		distro: distro.ZarfDistro{},
		tmp:    "/tmp",
	}

	return &dis, nil
}
