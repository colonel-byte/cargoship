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

package noncluster

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zarf-dev/zarf/src/pkg/utils/exec"
)

// TestAnsibleModuleStdout holds the module contract against the one path module mode cannot
// guard: package initialisation. src/internal/ansiblemod redirects os.Stdout at entry, but
// src/cmd builds the root command in a package-level variable, so a configuration file is read
// and reported on before main begins and before any redirection could be installed. Those
// writers go to stderr by audit rather than by construction, which is only true for as long as
// something checks. This is that check, and it needs the real binary because the writes happen
// once per process, before any test in src/cmd can run.
func TestAnsibleModuleStdout(t *testing.T) {
	t.Run("answers with one JSON object and nothing else", func(t *testing.T) {
		dir := t.TempDir()
		config := filepath.Join(dir, "cargoship-config.yaml")
		require.NoError(t, os.WriteFile(config, []byte("log_level: info\n"), 0o600))

		// The parameters are deliberately incomplete: a module that reports a bad parameter is
		// still a module answering, and reaching the phase pipeline would need a package and a
		// fleet. What is under test is the channel, not the action.
		stdout, _, err := cargoshipModule(t, "engine_config_sync", config, moduleArgs(t, dir, "{}"))
		require.NoError(t, err)

		require.NotContains(t, strings.TrimSpace(stdout), "\n", "a module writes exactly one line")

		var resp struct {
			Changed   bool   `json:"changed"`
			Failed    bool   `json:"failed"`
			Msg       string `json:"msg"`
			Cargoship struct {
				Module string `json:"module"`
			} `json:"cargoship"`
		}
		require.NoError(t, json.Unmarshal([]byte(stdout), &resp), "stdout is not the module result: %q", stdout)
		require.True(t, resp.Failed)
		require.Contains(t, resp.Msg, `the "package" parameter is required`)
		require.Equal(t, "engine_config_sync", resp.Cargoship.Module)
	})

	t.Run("a configuration file that cannot be used leaves stdout empty", func(t *testing.T) {
		// This file is reported on during package initialisation and then fails the strict load,
		// which exits before the module answers at all. Ansible reads that as a module failure
		// and shows stderr, which is the same failure the CLI gives for the same file. What must
		// not happen is the complaint landing on stdout, where it would reach Ansible as a
		// parse error naming nothing.
		dir := t.TempDir()
		config := filepath.Join(dir, "cargoship-config.yaml")
		require.NoError(t, os.WriteFile(config, []byte("log_level: [unclosed\n"), 0o600))

		stdout, stderr, err := cargoshipModule(t, "engine_config_sync", config, moduleArgs(t, dir, "{}"))
		require.Error(t, err)
		require.Empty(t, stdout)
		require.Contains(t, stderr, "failed to load config")
	})
}

// cargoshipModule runs the built binary as an Ansible module.
//
// The module is selected through CARGOSHIP_ANSIBLE_MODULE rather than a symlink, which is what
// the environment variable is for: the binary this suite builds is named cargoship_linux_amd64,
// and the prefix in that name is deliberately not enough to enter module mode.
func cargoshipModule(t *testing.T, module, config, args string) (string, string, error) {
	t.Helper()

	cfg := exec.PrintCfg()
	cfg.Env = []string{
		"CARGOSHIP_ANSIBLE_MODULE=" + module,
		"CARGOSHIP_CONFIG=" + config,
	}

	return exec.CmdWithTesting(t, cfg, e2e.CargoBinPath, args)
}

// moduleArgs writes the arguments file Ansible passes a WANT_JSON module and returns its path.
func moduleArgs(t *testing.T, dir, params string) string {
	t.Helper()

	path := filepath.Join(dir, "args.json")
	require.NoError(t, os.WriteFile(path, []byte(params), 0o600))

	return path
}
