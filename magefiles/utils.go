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

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/magefile/mage/sh"
)

func daggerBuildLocal(oper string, arch string) error {
	bin := fmt.Sprintf("build/cargoship_%s_%s", oper, arch)
	fmt.Println("building: " + bin)

	if err := os.Remove(bin); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	return sh.RunV(
		binaryPath("dagger"),
		"call",
		"--progress=tty",
		"--interactive=false",
		"build-local",
		"--os="+oper,
		"--arch="+arch,
		"export",
		fmt.Sprintf("--path=%s", bin),
	)
}

func clean() error {
	files, _ := filepath.Glob("build/cargoship_*")
	for _, f := range files {
		fmt.Println("removing: " + f)
		if err := os.Remove(f); err != nil {
			return err
		}
	}
	return nil
}
