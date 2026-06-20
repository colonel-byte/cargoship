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
	"fmt"
	"runtime"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

const (
	buildDir = "build"
)

var Default = Dagger.All

type (
	Dagger mg.Namespace
)

// Toolchain generates the dagger build environment
func (Dagger) Toolchain() error {
	return sh.RunV(
		"dagger",
		"toolchain",
		"update",
	)
}

// Binary will build a binary of the local system, with dagger
func (Dagger) Binary() error {
	return daggerBuildLocal(runtime.GOOS, runtime.GOARCH)
}

// Linuxamd64 build a linux amd64 binary, with dagger
func (Dagger) Linuxamd64() error {
	return daggerBuildLocal("linux", "amd64")
}

// Linuxarm64 build a linux arm64 binary, with dagger
func (Dagger) Linuxarm64() error {
	return daggerBuildLocal("linux", "arm64")
}

// Macamd64 build a mac amd64 binary, with dagger
func (Dagger) Macamd64() error {
	return daggerBuildLocal("darwin", "amd64")
}

// Macarm64 build a mac arm64 binary, with dagger
func (Dagger) Macarm64() error {
	return daggerBuildLocal("darwin", "arm64")
}

// All builds all cargoship binaries, with dagger
func (Dagger) All() error {
	if err := clean(); err != nil {
		return err
	}
	return sh.RunV(
		"dagger",
		"call",
		"--progress=tty",
		"--interactive=false",
		"build",
		"export",
		fmt.Sprintf("--path=%s", buildDir),
	)
}
