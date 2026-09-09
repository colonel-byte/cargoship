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
	"strings"

	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/pkg/utils/build"
	"github.com/magefile/mage/sh"
	"golang.org/x/term"
)

// daggerProgressFlag picks a tty progress bar when stdout is an actual terminal, falling
// back to dagger's plain log output otherwise (e.g. CI, or any other non-interactive shell)
// where a tty renderer would just error out with "no tty available".
func daggerProgressFlag() string {
	if term.IsTerminal(int(os.Stdout.Fd())) {
		return "--progress=tty"
	}
	return "--progress=plain"
}

func daggerBuildLocal(oper string, arch string) error {
	bin := fmt.Sprintf("build/cargoship_%s_%s", oper, arch)
	fmt.Println("building: " + bin)

	if err := os.Remove(bin); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	return sh.RunV(
		"dagger",
		"call",
		daggerProgressFlag(),
		"--interactive=false",
		"build-local",
		"--os="+oper,
		"--arch="+arch,
		"export",
		fmt.Sprintf("--path=%s", bin),
	)
}

func hostBuildLocal(oper string, arch string) error {
	bin := fmt.Sprintf("build/cargoship_%s_%s", oper, arch)
	fmt.Println("building: " + bin)

	if err := os.Remove(bin); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	env := map[string]string{}
	env["GOOS"] = oper
	env["GOARCH"] = arch
	env["CGO_ENABLED"] = "0"

	gc := build.GCFLags()
	ld := build.LDFlags(config.UnsetCLIVersion, gitCommit())

	goBuild := fmt.Sprintf(`go build -a -trimpath -gcflags=all="%s" -ldflags "%s" -o %s ./main.go`, gc, ld, bin)

	fmt.Println("executing:\n  " + goBuild)

	return sh.RunWithV(
		env,
		"sh",
		"-c",
		goBuild,
	)
}

// gitCommit returns the short commit of the checkout being built, suffixed with "-dirty" when
// the working tree has uncommitted changes. When the commit cannot be resolved, for example in a
// source tree with no .git directory or on a machine without git, it returns
// config.UnsetCLICommit so a local build always stamps something rather than an empty string.
func gitCommit() string {
	commit, err := sh.Output("git", "rev-parse", "--short", "HEAD")
	if err != nil {
		return config.UnsetCLICommit
	}

	commit = strings.TrimSpace(commit)
	if commit == "" {
		return config.UnsetCLICommit
	}

	if dirty, err := sh.Output("git", "status", "--porcelain"); err == nil && strings.TrimSpace(dirty) != "" {
		commit += "-dirty"
	}

	return commit
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
