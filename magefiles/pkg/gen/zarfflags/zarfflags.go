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

// Package zarfflags records the flag surface of the zarf commands the Ansible modules wrap.
//
// internal/zarfmod renders a zarf command line, and a flag it renders that zarf does not accept
// fails the run with a usage error that reads as the module being broken rather than as a
// parameter being wrong. TestInitArgsAreKnownFlags and TestDeployArgsAreKnownFlags catch that, by
// holding every flag the modules can render against a recorded list -- and a recorded list is only
// as true as the last person to copy it out of the documentation.
//
// This reads the list off the installed zarf instead, so refreshing it is a command rather than an
// afternoon. It does not read it at test time on purpose: the unit tests run with no zarf on the
// machine, in CI and on a contributor's laptop, and a test that needs a binary to say anything is
// a test that gets skipped.
package zarfflags

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/magefile/mage/sh"
)

// Dir is where the recorded lists live, beside the tests that read them.
var Dir = filepath.Join("internal", "zarfmod", "testdata")

// EnvBinary names the zarf to read, for a machine where it is not on PATH or where two versions
// are installed side by side. It is the same variable the modules themselves read.
const EnvBinary = "CARGOSHIP_ZARF_BINARY"

// target is one zarf command whose flags are recorded.
type target struct {
	// args is the command, as zarf's own argument vector.
	args []string
	// file is the list's name under Dir.
	file string
	// test names the test that reads the list, for the header.
	test string
	// docs is the upstream page documenting the same flags, also for the header.
	docs string
}

// targets are the commands the Ansible modules wrap. A module added to internal/zarfmod needs an
// entry here, and the test that holds its flags against the list is what notices if it has none.
func targets() []target {
	return []target{
		{
			args: []string{"init"},
			file: "zarf-init-flags.txt",
			test: "TestInitArgsAreKnownFlags",
			docs: "https://docs.zarf.dev/commands/zarf_init/",
		},
		{
			args: []string{"package", "deploy"},
			file: "zarf-package-deploy-flags.txt",
			test: "TestDeployArgsAreKnownFlags",
			docs: "https://docs.zarf.dev/commands/zarf_package_deploy/",
		},
	}
}

// flagLine matches a flag as cobra's help renders it: indented, optionally a short form first,
// then the long form. The flag's type and description follow on the same line and are not
// recorded -- what the tests ask is whether a name is accepted.
var flagLine = regexp.MustCompile(`^\s+(?:-[a-zA-Z], )?--([a-zA-Z0-9][a-zA-Z0-9-]*)`)

// Generate rewrites every recorded flag list from the installed zarf.
func Generate() error {
	binary := binary()
	version, err := version(binary)
	if err != nil {
		return err
	}

	for _, t := range targets() {
		flags, err := read(binary, t)
		if err != nil {
			return err
		}
		if err := write(t, flags, binary, version); err != nil {
			return err
		}
		fmt.Printf("recorded %d flags for `zarf %s` from %s\n", len(flags), strings.Join(t.args, " "), version)
	}
	return nil
}

// binary is the zarf to read: the environment, then PATH.
func binary() string {
	if env := os.Getenv(EnvBinary); env != "" {
		return env
	}
	return "zarf"
}

// version is what the installed zarf reports, recorded in each header so a reader knows which
// release the list describes.
func version(binary string) (string, error) {
	out, err := sh.Output(binary, "version")
	if err != nil {
		return "", fmt.Errorf("unable to run %s version: install zarf, or name one with %s: %w", binary, EnvBinary, err)
	}
	version := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
	if version == "" {
		return "", fmt.Errorf("%s version printed nothing", binary)
	}
	return version, nil
}

// read returns the flags one command accepts, sorted and deduplicated.
//
// Both the command's own flags and the global ones it inherits are taken, because the modules
// render both and zarf accepts both in the same vector. The two sections are not distinguished in
// the output for the same reason.
func read(binary string, t target) ([]string, error) {
	args := append(append([]string{}, t.args...), "--help")
	out, err := sh.Output(binary, args...)
	if err != nil {
		return nil, fmt.Errorf("unable to run %s %s: %w", binary, strings.Join(args, " "), err)
	}

	flags := parseHelp(out)
	if len(flags) == 0 {
		return nil, fmt.Errorf("%s %s listed no flags, so the help output is not the shape this parses",
			binary, strings.Join(args, " "))
	}
	return flags, nil
}

// parseHelp returns the flag names in cobra help output, sorted and deduplicated.
//
// Deduplicated because a command can list a flag in its own section and inherit one of the same
// name, and sorted because the recorded list is read by eye as often as by a test.
func parseHelp(help string) []string {
	seen := map[string]bool{}
	scanner := bufio.NewScanner(strings.NewReader(help))
	for scanner.Scan() {
		if m := flagLine.FindStringSubmatch(scanner.Text()); m != nil {
			seen[m[1]] = true
		}
	}

	flags := make([]string, 0, len(seen))
	for flag := range seen {
		flags = append(flags, flag)
	}
	sort.Strings(flags)
	return flags
}

// write renders one list, header and all. The header carries the zarf the list came from and the
// date it was read, so a list that has drifted can be told from one that was never refreshed.
func write(t target, flags []string, binary, version string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# The flags `zarf %s` accepts, including the global flags it inherits.\n", strings.Join(t.args, " "))
	b.WriteString("#\n")
	b.WriteString("# Generated by `mage generate:zarfFlags` -- do not edit by hand.\n")
	fmt.Fprintf(&b, "# Read from: %s %s --help\n", binary, strings.Join(t.args, " "))
	fmt.Fprintf(&b, "# Zarf version: %s\n", version)
	fmt.Fprintf(&b, "# Recorded: %s\n", time.Now().UTC().Format(time.DateOnly))
	fmt.Fprintf(&b, "# Upstream reference: %s\n", t.docs)
	b.WriteString("#\n")
	fmt.Fprintf(&b, "# %s holds every flag the module renders against this list. It is recorded rather than read\n", t.test)
	b.WriteString("# at test time because the unit tests run with no zarf on the machine, and a test that needs a\n")
	b.WriteString("# binary to say anything is a test that gets skipped. Refresh it when the zarf the collection\n")
	b.WriteString("# targets moves, and the test will name any flag that went away.\n")
	b.WriteString("#\n")
	b.WriteString("# One flag per line. Blank lines and lines beginning with # are ignored.\n")
	b.WriteString("\n")
	for _, flag := range flags {
		b.WriteString(flag)
		b.WriteString("\n")
	}

	path := filepath.Join(Dir, t.file)
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return fmt.Errorf("unable to write %s: %w", path, err)
	}
	fmt.Println(path)
	return nil
}
