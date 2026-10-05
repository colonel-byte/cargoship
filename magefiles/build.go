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
	"runtime"

	"github.com/colonel-byte/cargoship/magefiles/pkg/build"
	"github.com/magefile/mage/mg"
)

var Default = Build.All

type (
	Build  mg.Namespace
	Binary mg.Namespace
)

// Binary will build a binary of the local system, or for the specified OS and architecture
func (Build) Binary(
	// target OS (defaults to host runtime.GOOS)
	os *string,
	// target architecture (defaults to host runtime.GOARCH)
	arch *string,
) error {
	targetOS := runtime.GOOS
	if os != nil && *os != "" {
		targetOS = *os
	}
	targetArch := runtime.GOARCH
	if arch != nil && *arch != "" {
		targetArch = *arch
	}
	return build.Binary(targetOS, targetArch)
}

// All builds all cargoship binaries, on the host
func (Build) All() error {
	return build.All()
}

// Examples builds a package from every example definition, with the cargoship on PATH.
func (Build) Examples() error {
	return build.Examples()
}
