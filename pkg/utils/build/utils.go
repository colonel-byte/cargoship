// Copyright 2023 harbor-cli authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from harbor-cli:
// https://github.com/goharbor/harbor-cli
//
// Modifications Copyright 2026 colonel-byte.
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

// Package build is the build flag logic shared across mage and the CLI
package build

import (
	"fmt"
	"strings"
)

// LDFlags will return the ld flags needed to build this binary
func LDFlags(version string, commit string) string {
	return strings.TrimSpace(
		fmt.Sprintf(
			strings.Join(
				[]string{
					"-s",
					"-w",
					"-X github.com/colonel-byte/cargoship/config.CLIVersion=%s",
					"-X github.com/colonel-byte/cargoship/config.CLICommit=%s",
				},
				" ",
			),
			strings.TrimSpace(version),
			strings.TrimSpace(commit),
		),
	)
}

// GCFLags will return the gc flags needed to build this binary.
//
// Only -l (disable inlining) is set, and it is set for its size effect:
// inlining copies a function body to every call site, which across this
// repo's dependency tree costs far more space than it buys a CLI that is not
// CPU-bound.
//
// Two flags that used to be here were removed deliberately, see
// docs/dev/build-flags.md:
//
//   - -B (disable bounds checking) bought about 1% of binary size in exchange
//     for removing bounds checks from every package in the tree, including the
//     code that parses untrusted archives and registry responses. That is where
//     a bounds check is what turns a malformed length field into a panic rather
//     than an out-of-bounds read.
//   - -C does nothing for size. `go tool compile -help` documents it as
//     "disable printing of columns in error messages" -- a diagnostic
//     formatting flag with no effect on generated code.
func GCFLags() string {
	return "-l"
}
