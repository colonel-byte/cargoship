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

// Package fuzz holds the fuzz targets for the vault code and for the Ansible inventory
// translation. They assert properties that have to hold
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
// Everything that encrypts runs against both formats cargoship writes, Ansible Vault and age. The
// keyrings are built once in TestMain; see keyring_test.go. The Ansible targets in
// ansible_inventory_fuzz_test.go need no key material at all: they fuzz the inventory an Ansible
// module is handed, where a wrong answer is a cluster with the wrong topology rather than a file
// that fails to parse.
//
// Run one target:
//
//	go test -run=Fuzz -fuzz=FuzzDecryptAtPathRoundTrip -fuzztime=60s ./src/test/e2e/fuzz
//
// A plain `go test ./src/test/e2e/fuzz` runs the seed corpus only -- the f.Add values in each
// target plus anything committed under testdata/fuzz/<target>/ -- which is what turns a crash
// found by a long fuzz run into a permanent regression test once its file is committed.
package fuzz
