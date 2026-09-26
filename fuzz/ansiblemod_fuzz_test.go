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

package fuzz

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/internal/ansiblemod"
	"github.com/stretchr/testify/require"
)

// FuzzReadArgsNoPanic asserts that ReadArgs never panics reading the arguments file Ansible writes
// for a WANT_JSON module.
//
// The file crosses from a control node running whatever version of Ansible it runs, decoded
// leniently on purpose so a control key added between releases does not fail a module that has not
// been updated to know it -- and every byte in it is data this binary did not write.
func FuzzReadArgsNoPanic(f *testing.F) {
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"cluster_config": "path", "_ansible_check_mode": true}`))
	f.Add([]byte(`not json`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"_ansible_verbosity": "not a number"}`))
	f.Add([]byte(`{"_ansible_unknown_future_key": {"nested": [1, 2, 3]}}`))
	f.Add([]byte(``))
	f.Add([]byte(`{"a": 1, "a": 2}`))

	f.Fuzz(func(t *testing.T, contents []byte) {
		dir := t.TempDir()
		path := filepath.Join(dir, "args.json")
		require.NoError(t, os.WriteFile(path, contents, 0o600))

		_, _ = ansiblemod.ReadArgs(path) //nolint:errcheck // no-panic fuzz test, malformed input is expected to error
	})
}

// FuzzModuleNameNoPanic asserts that ModuleName never panics on an arbitrary argv[0], which Ansible
// (or a symlink an operator made) is free to make anything at all.
func FuzzModuleNameNoPanic(f *testing.F) {
	f.Add("cargoship_apply")
	f.Add("AnsiballZ_cargoship_apply")
	f.Add("cargoship_linux_amd64")
	f.Add("cargoship_")
	f.Add("AnsiballZ_")
	f.Add("")
	f.Add("/usr/bin/cargoship_prepare")
	f.Add("cargoship_" + string([]byte{0, 1, 2}))

	f.Fuzz(func(_ *testing.T, argv0 string) {
		_, _ = ansiblemod.ModuleName(argv0)
	})
}
