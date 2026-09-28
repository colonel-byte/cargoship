//go:build mage

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

package main

import (
	"github.com/magefile/mage/sh"
)

// Fuzz replays the fuzz seed corpus: the f.Add values in each target plus anything committed
// under fuzz/testdata/fuzz/<Target>/. It is its own target rather than part of EndToEnd
// because the package is not an e2e suite -- it calls the packages in process and needs no
// binary, no cluster and no network, so this is a second or so rather than an hour.
//
// This replays the corpus; it does not fuzz. Actual fuzzing takes one target at a time and a
// -fuzztime budget, which is a loop rather than a target. See docs/dev/fuzz-tests.md.
func (Test) Fuzz() error {
	return sh.RunV("go", "test", "-count=1", "./fuzz/...")
}

// unitPackages is everything Test.Unit covers: the whole module except the two suites that
// have targets of their own. ./test/... is the e2e suite Test.EndToEnd runs, which needs
// Docker and an hour; ./fuzz/... is the seed corpus Test.Fuzz replays.
//
// Listed rather than excluded because go test has no exclusion syntax, and a new top-level
// package that nothing runs is a worse failure than one line to add here.
var unitPackages = []string{
	"./api/...",
	"./cmd/...",
	"./config/...",
	"./internal/...",
	"./magefiles/pkg/...",
	"./pkg/...",
	"./types/...",
}

// Unit runs the unit tests: everything that needs no cluster, no Docker and no network.
func (Test) Unit() error {
	args := append([]string{"test", "-count=1"}, unitPackages...)
	return sh.RunV("go", args...)
}
