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

//go:build mage
// +build mage

package main

import "github.com/magefile/mage/sh"

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
