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

// Package fuzz holds the fuzz targets for the vault code. They assert properties that have to hold
// for every input rather than for the handful of values a table test names: that a credential
// survives an encrypt/decrypt round trip unchanged, that rewriting one value in a configuration
// leaves every other byte alone, and that a path the encryptor accepts is one the apply-time
// decryptor reads.
//
// The targets call clustercfg in process rather than driving the built binary, which is what lets
// them run at tens of thousands of executions per second and reach the encoding decisions --
// scalar style, quoting, indentation, byte offsets -- where a wrong answer is silent. Nothing here
// needs the binary under build/, a cluster, or the network.
//
// Run one target:
//
//	go test -run=Fuzz -fuzz=FuzzDecryptAtPathRoundTrip -fuzztime=60s ./src/test/e2e/fuzz
//
// A plain `go test ./src/test/e2e/fuzz` runs the seed corpus only -- the f.Add values in each
// target plus anything committed under testdata/fuzz/<target>/ -- which is what turns a crash
// found by a long fuzz run into a permanent regression test once its file is committed.
package fuzz

import "github.com/colonel-byte/cargoship/src/internal/clustercfg"

// fuzzPassword is the vault password every target in this file encrypts under. These targets fuzz
// the value and the path, not the password: a wrong password is covered by the table tests in
// clustercfg, and varying it here would spend most of the corpus on inputs that fail for the
// uninteresting reason.
const fuzzPassword = "correct horse battery staple"

// fuzzKeyring is fuzzPassword in the form the encrypt and decrypt entry points take. The raw
// password is kept for the rekey target, which needs a second keyring to rotate to.
var fuzzKeyring = clustercfg.NewVaultKeyring(fuzzPassword)
