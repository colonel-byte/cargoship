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
)

type (
	Build  mg.Namespace
	Binary mg.Namespace
)

// Binary will build a binary of the local system, on the host
func (Build) Binary() error {
	return hostBuildLocal(runtime.GOOS, runtime.GOARCH)
}

// Linuxamd64 build a linux amd64 binary, on the host
func (Build) Linuxamd64() error {
	return hostBuildLocal("linux", "amd64")
}

// Linuxarm64 build a linux arm64 binary, on the host
func (Build) Linuxarm64() error {
	return hostBuildLocal("linux", "arm64")
}

// Macamd64 build a mac amd64 binary, on the host
func (Build) Macamd64() error {
	return hostBuildLocal("darwin", "amd64")
}

// Macarm64 build a mac arm64 binary, on the host
func (Build) Macarm64() error {
	return hostBuildLocal("darwin", "arm64")
}

// All builds all cargoship binaries, on the host
func (b Build) All() error {
	return runSquential(
		b.Linuxamd64,
		b.Linuxarm64,
		b.Macamd64,
		b.Macarm64,
	)
}

func runSquential(funcs ...func() error) error {
	for _, f := range funcs {
		if err := f(); err != nil {
			fmt.Printf("got an error: %v", err)
		}
	}
	return nil
}
