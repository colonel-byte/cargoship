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
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/colonel-byte/cargoship/src/internal/ansiblemod"
)

// ansibleArgsFixture is the inventory an Ansible action plugin projects into a module's arguments:
// Ansible's own groups and hostvars, plus the cluster settings an Ansible inventory cannot carry.
// It has no parameters of its own, so each case adds the ones it needs to a copy.
//
// Nothing in it is contacted. Every case here either runs under check mode or fails on a package
// that is not there, both of which happen before cargoship opens a connection.
const ansibleArgsFixture = "src/test/e2e/noncluster/testdata/ansible-module-args.json"

// missingPackage is a path that does not exist, so a module fails while loading the package. That
// is after the inventory has been translated and written, which is the part under test.
const missingPackage = "/nonexistent/cargoship-e2e-package.tar.zst"

// TestAnsibleModuleContract drives the binary the way Ansible drives it.
//
// The contract is narrow and entirely about the process: the module is selected by the name the
// binary is invoked under, its whole result is one JSON object on stdout, and it exits 0 even when
// it failed, because a non-zero status makes Ansible report its own diagnosis instead of the
// message the module wrote. None of that can be tested in-process, which is why it is here.
func TestAnsibleModuleContract(t *testing.T) {
	t.Run("answers check mode with one JSON object and exits 0", func(t *testing.T) {
		args := ansibleArgs(t, map[string]any{
			"distro":              "rke2",
			"_ansible_check_mode": true,
		})

		stdout, _, exit := runAnsibleModule(t, "cargoship_kube_config", args, nil)
		require.Equal(t, 0, exit)

		result := oneJSONObject(t, stdout)
		require.Equal(t, true, result["skipped"], "kube-config has no dry run, so check mode skips")
		require.Equal(t, false, result["changed"])
		require.Nil(t, result["failed"])

		detail, ok := result["cargoship"].(map[string]any)
		require.True(t, ok, "the result carries no cargoship detail")
		require.Equal(t, "kube_config", detail["module"])
		require.Equal(t, true, detail["checkMode"])
	})

	t.Run("answers under the name Ansible copies it to", func(t *testing.T) {
		// This is the name every real module run arrives under. Ansible copies the module file to
		// the machine that runs it and prefixes the copy, so the cargoship_kube_config symlink an
		// operator sees in the collection is not the name the process is given.
		args := ansibleArgs(t, map[string]any{"distro": "rke2", "_ansible_check_mode": true})

		stdout, _, exit := runAnsibleModule(t, "AnsiballZ_cargoship_kube_config", args, nil)
		require.Equal(t, 0, exit)
		require.Equal(t, true, oneJSONObject(t, stdout)["skipped"])
	})

	t.Run("reports a broken config file as a module failure", func(t *testing.T) {
		// The config file is read during package initialisation, before any flag is parsed and
		// before main runs. That is the oldest thing in the process that can write to stdout or
		// end it, so it is the one worth provoking: the module still has to answer with one JSON
		// object and exit 0.
		broken := filepath.Join(t.TempDir(), "cargoship-config.yaml")
		require.NoError(t, os.WriteFile(broken, []byte("log_level: [this is not a scalar\n"), 0o600))
		t.Setenv("CARGOSHIP_CONFIG", broken)

		// The file is only loaded once a command actually runs, so this case has to be one that
		// runs one. A check-mode skip never gets that far.
		args := ansibleArgs(t, map[string]any{"package": missingPackage})

		stdout, stderr, exit := runAnsibleModule(t, "cargoship_engine_config_sync", args, nil)
		require.Equal(t, 0, exit)

		result := oneJSONObject(t, stdout)
		require.Equal(t, true, result["failed"])
		require.Contains(t, result["msg"], broken, "the module names the file it could not use")
		require.Contains(t, stderr, "failed to load config")
	})

	t.Run("translates the inventory and runs the command over it", func(t *testing.T) {
		inventory := filepath.Join(t.TempDir(), "generated-inventory.yaml")
		args := ansibleArgs(t, map[string]any{
			"package":        missingPackage,
			"inventory_path": inventory,
		})

		stdout, stderr, exit := runAnsibleModule(t, "cargoship_engine_config_sync", args, nil)
		require.Equal(t, 0, exit, "a failed module still exits 0")

		result := oneJSONObject(t, stdout)
		require.Equal(t, true, result["failed"])
		require.Contains(t, result["msg"], missingPackage,
			"the module reports cargoship's own error, not one of its own")

		detail, ok := result["cargoship"].(map[string]any)
		require.True(t, ok, "the result carries no cargoship detail")
		require.Equal(t, inventory, detail["inventoryPath"])
		require.Equal(t, true, detail["inventoryKept"], "a failed run leaves the document to look at")
		require.Contains(t, detail["command"], "--config")
		require.Contains(t, detail["command"], inventory)

		// The document is the whole point of the translation, so read it rather than trusting the
		// path. The fixture puts kc01 first in the controller group, and db01 in no mapped group.
		generated, err := os.ReadFile(inventory)
		require.NoError(t, err)
		document := string(generated)
		require.Contains(t, document, "hostname: distro-kc01")
		require.Contains(t, document, "role: controller")
		require.Contains(t, document, "role: worker")
		require.NotContains(t, document, "10.1.2.9", "a host in no mapped group is not in the cluster")
		require.Less(t, strings.Index(document, "10.1.2.3"), strings.Index(document, "10.1.2.5"),
			"controllers come first, and the first of them is the leader")

		require.NotEmpty(t, stderr, "cargoship's own logging goes to stderr")
	})

	t.Run("names a parameter it does not know", func(t *testing.T) {
		args := ansibleArgs(t, map[string]any{"packge": missingPackage})

		stdout, _, exit := runAnsibleModule(t, "cargoship_engine_config_sync", args, nil)
		require.Equal(t, 0, exit)

		result := oneJSONObject(t, stdout)
		require.Equal(t, true, result["failed"])
		require.Contains(t, result["msg"], `"packge"`, "an unknown parameter is reported by name")
		require.Contains(t, result["msg"], `"package"`, "and the module says what it does accept")
	})

	// The prefix alone does not make a module. This repository builds its own binary as
	// cargoship_linux_amd64, and every other test in this suite runs that file, so a binary that
	// went into module mode on the prefix would answer JSON to every one of them.
	t.Run("runs the CLI under a name that is not a module", func(t *testing.T) {
		stdout, stderr, exit := runAnsibleModule(t, "cargoship_linux_amd64", "version", nil)
		require.Equal(t, 0, exit, "stderr: %s", stderr)
		require.NotContains(t, stdout, "cargoship Ansible module")
		require.NotEmpty(t, strings.TrimSpace(stdout), "version printed nothing")
		require.False(t, strings.HasPrefix(strings.TrimSpace(stdout), "{"), "the CLI wrote a module result")
	})

	t.Run("names the modules it answers as", func(t *testing.T) {
		args := ansibleArgs(t, map[string]any{})

		env := []string{ansiblemod.EnvModule + "=install"}
		stdout, _, exit := runAnsibleModule(t, "cargoship_kube_config", args, env)
		require.Equal(t, 0, exit)

		result := oneJSONObject(t, stdout)
		require.Equal(t, true, result["failed"])
		require.Contains(t, result["msg"], `"cargoship_install" is not a cargoship Ansible module`)
		require.Contains(t, result["msg"], `"apply"`)
	})

	t.Run("reports a missing arguments file rather than running the CLI", func(t *testing.T) {
		stdout, _, exit := runAnsibleModule(t, "cargoship_apply", "", nil)
		require.Equal(t, 0, exit)

		result := oneJSONObject(t, stdout)
		require.Equal(t, true, result["failed"])
		require.Contains(t, result["msg"], "WANT_JSON")
	})
}

// ansibleArgs writes a module arguments file: the fixture inventory plus the given parameters. It
// returns the path, which is what Ansible passes a WANT_JSON module as its one argument.
func ansibleArgs(t *testing.T, params map[string]any) string {
	t.Helper()

	fixture, err := os.ReadFile(ansibleArgsFixture)
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(fixture, &doc))
	for name, value := range params {
		doc[name] = value
	}

	encoded, err := json.Marshal(doc)
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "args.json")
	require.NoError(t, os.WriteFile(path, encoded, 0o600))
	return path
}

// runAnsibleModule runs the binary under the given module name, with args as its one argument. An
// empty args runs it with no arguments at all.
//
// The name is given by a symlink rather than by the environment variable the package also accepts,
// because the symlink is what Ansible actually produces and is the thing worth testing. The env
// entries are added to the environment of this process, for the cases that need the variable.
func runAnsibleModule(t *testing.T, name, args string, env []string) (string, string, int) {
	t.Helper()

	module := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.Symlink(e2e.CargoBinPath, module))

	var argv []string
	if args != "" {
		argv = append(argv, args)
	}

	cmd := exec.Command(module, argv...) //nolint:gosec // the path is this test's own symlink
	cmd.Env = append(os.Environ(), env...)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		require.NoError(t, err, "running the module: %s", stderr.String())
	}
	return stdout.String(), stderr.String(), cmd.ProcessState.ExitCode()
}

// oneJSONObject parses stdout as exactly one JSON object.
//
// Exactly one is the contract: Ansible reads the whole of a module's stdout and parses it, so a
// second document, a log line, or a trailing banner makes the run fail with Ansible's own message
// rather than the module's.
func oneJSONObject(t *testing.T, stdout string) map[string]any {
	t.Helper()

	decoder := json.NewDecoder(strings.NewReader(stdout))
	var result map[string]any
	require.NoError(t, decoder.Decode(&result), "stdout is not one JSON object: %q", stdout)
	require.False(t, decoder.More(), "stdout carries more than the result: %q", stdout)
	return result
}
