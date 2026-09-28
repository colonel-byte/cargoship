// Copyright 2026 colonel-byte
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

// Package hostbuild compiles the cargoship binary with the host's Go toolchain.
//
// It is the layer behind the Build namespace and behind the e2e suites that drive a freshly built
// binary. See docs/dev/build-flags.md for what each flag and environment variable does.
package hostbuild

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/utils/build"
	"github.com/magefile/mage/sh"
)

// Dir is where every binary this package writes lands, relative to the repository root.
const Dir = "build"

// Platform fills in either half of an OS/arch pair the caller left empty with the host's own
// value, so an invocation can name only the axis it wants to change.
func Platform(oper string, arch string) (string, string) {
	if oper == "" {
		oper = runtime.GOOS
	}
	if arch == "" {
		arch = runtime.GOARCH
	}
	return oper, arch
}

// ArgsAfter returns the arguments following target on a mage command line.
//
// A target has no way to ask which of os.Args are its own: mage's own flags come before the
// target name and several of them take a value, so counting leading flags cannot tell the target
// name from a flag's argument. Finding the target name itself is unambiguous, and matching is
// case-insensitive because mage's own target lookup is.
func ArgsAfter(argv []string, target string) []string {
	for i, arg := range argv {
		if strings.EqualFold(arg, target) {
			return argv[i+1:]
		}
	}
	return nil
}

// ParsePlatform reads the -os and -arch flags a build target accepts, returning the platform to
// build and how many arguments were consumed. Either flag may be omitted, in which case that half
// of the pair is the host's own -- see Platform.
//
// Both the -os=darwin and -os darwin spellings work, as does a leading double dash, because this
// is the standard library's flag parser rather than a hand-rolled one. Parsing stops at the first
// argument that is not a flag, so anything after that is left for the caller to deal with.
func ParsePlatform(args []string) (oper string, arch string, consumed int, err error) {
	// ContinueOnError with the output discarded: an unknown flag is returned as an error to be
	// reported like any other target failure, rather than printed to stderr and re-printed by
	// mage, and the flag package must not call os.Exit out from under a build.
	flags := flag.NewFlagSet("build:binary", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&oper, "os", "", "the GOOS to build for; defaults to this host's")
	flags.StringVar(&arch, "arch", "", "the GOARCH to build for; defaults to this host's")

	if err := flags.Parse(args); err != nil {
		return "", "", 0, fmt.Errorf("%w (accepted: -os=<goos> -arch=<goarch>)", err)
	}

	oper, arch = Platform(oper, arch)
	return oper, arch, len(args) - flags.NArg(), nil
}

// Local builds the binary for one OS/arch pair into Dir.
func Local(oper string, arch string) error {
	bin := fmt.Sprintf("%s/cargoship_%s_%s", Dir, oper, arch)
	fmt.Println("building: " + bin)

	if err := os.Remove(bin); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	env := map[string]string{}
	env["GOOS"] = oper
	env["GOARCH"] = arch
	env["CGO_ENABLED"] = "0"

	gc := build.GCFLags()
	ld := build.LDFlags(config.UnsetCLIVersion, GitCommit())

	// No -a here: the Go build cache is keyed on build flags, so a change to
	// -gcflags/-ldflags already rebuilds what it affects. -a only forced every
	// build to redo the whole standard library. See docs/dev/build-flags.md.
	goBuild := fmt.Sprintf(`go build -trimpath -gcflags=all="%s" -ldflags "%s" -o %s ./main.go`, gc, ld, bin)

	fmt.Println("executing:\n  " + goBuild)

	return sh.RunWithV(
		env,
		"sh",
		"-c",
		goBuild,
	)
}

// GitCommit returns the short commit of the checkout being built, suffixed with "-dirty" when
// the working tree has uncommitted changes. When the commit cannot be resolved, for example in a
// source tree with no .git directory or on a machine without git, it returns
// config.UnsetCLICommit so a local build always stamps something rather than an empty string.
func GitCommit() string {
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

// Clean removes every binary Local has written into Dir.
func Clean() error {
	// The only error Glob reports is a malformed pattern, and this one is a constant, but the
	// linter checks discarded errors and a returned one reads better than a justification.
	files, err := filepath.Glob(Dir + "/cargoship_*")
	if err != nil {
		return err
	}
	for _, f := range files {
		fmt.Println("removing: " + f)
		if err := os.Remove(f); err != nil {
			return err
		}
	}
	return nil
}
