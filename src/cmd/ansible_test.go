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

package cmd

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/colonel-byte/cargoship/src/internal/ansiblemod"
	"github.com/stretchr/testify/require"
)

// moduleArgs holds a fully populated parameter document for each module, so that the vector the
// parse below is handed is the widest one a playbook can produce.
var moduleArgs = map[string]string{
	"engine_config_sync": `{
		"package": "./package.tar.zst",
		"inventory": ` + moduleInventory + `,
		"concurrency": 4,
		"work_concurrency": "25%",
		"label_nodes": true,
		"update_kubeconfig": true,
		"kubeconfig": "/tmp/kubeconfig",
		"values": ["/tmp/values.yaml"],
		"timeout": "45m",
		"public_key": "/tmp/cosign.pub",
		"verify": "always",
		"log_level": "debug",
		"log_format": "json",
		"vault_password_file": "/tmp/vault-pass",
		"age_identity_file": ["/tmp/age.key"],
		"_ansible_check_mode": true
	}`,
	"apply": `{
		"package": "./package.tar.zst",
		"inventory": ` + moduleInventory + `,
		"concurrency": 4,
		"work_concurrency": "25%",
		"hosts": true,
		"firewall": false,
		"fapolicyd": true,
		"label_nodes": true,
		"allow_unmanaged_nodes": true,
		"update_kubeconfig": true,
		"kubeconfig": "/tmp/kubeconfig",
		"values": ["/tmp/values.yaml"],
		"timeout": "45m",
		"public_key": "/tmp/cosign.pub",
		"verify": "always",
		"log_level": "debug",
		"log_format": "json",
		"vault_password_file": "/tmp/vault-pass",
		"age_identity_file": ["/tmp/age.key"],
		"_ansible_check_mode": true
	}`,
	"prepare": `{
		"package": "./package.tar.zst",
		"inventory": ` + moduleInventory + `,
		"concurrency": 4,
		"work_concurrency": "25%",
		"hosts": true,
		"firewall": false,
		"fapolicyd": true,
		"values": ["/tmp/values.yaml"],
		"timeout": "45m",
		"public_key": "/tmp/cosign.pub",
		"verify": "always",
		"log_level": "debug",
		"log_format": "json",
		"_ansible_check_mode": true
	}`,
	"reset": `{
		"inventory": ` + moduleInventory + `,
		"distro": "rke2",
		"concurrency": 4,
		"work_concurrency": "25%",
		"hosts": true,
		"firewall": false,
		"fapolicyd": true,
		"log_level": "debug",
		"log_format": "json",
		"_ansible_check_mode": true
	}`,
	"kube_config": `{
		"inventory": ` + moduleInventory + `,
		"distro": "rke2",
		"kubeconfig": "/tmp/kubeconfig",
		"log_level": "debug",
		"log_format": "json"
	}`,
}

// moduleInventory is the smallest inventory that translates and validates.
const moduleInventory = `{
	"groups": {"controller": ["kc01"], "worker": ["kw01"]},
	"cluster": {"name": "bubbles", "loadbalancer": "bubbles-kc.test.com"}
}`

// TestModuleArgsParse is what keeps the flag names in src/internal/ansiblemod true.
//
// That package cannot import this one -- this one imports it -- so it spells the flags out. A
// rename here would otherwise be found by an operator, at the point a playbook failed against a
// live fleet, rather than by CI. Parsing each module's widest vector against the real command
// closes that: a flag that no longer exists, or that changed type, fails here.
func TestModuleArgsParse(t *testing.T) {
	for _, tt := range []struct {
		module  string
		command string
		// confirm is the command's confirmation flag, empty for a command without one.
		confirm string
		// dryRun is whether check mode should have rendered a dry run.
		dryRun bool
	}{
		{module: "engine_config_sync", command: "engine-config-sync", confirm: InstallEngineConfigSyncConfirm, dryRun: true},
		{module: "apply", command: "apply", confirm: InstallConfirm, dryRun: true},
		{module: "prepare", command: "prepare", confirm: InstallConfirm, dryRun: true},
		{module: "reset", command: "reset", confirm: InstallResetConfirm, dryRun: true},
		{module: "kube_config", command: "kube-config"},
	} {
		t.Run(tt.module, func(t *testing.T) {
			argsPath := filepath.Join(t.TempDir(), "args.json")
			require.NoError(t, os.WriteFile(argsPath, []byte(moduleArgs[tt.module]), 0o600))

			var argv []string
			code := withStdout(t, func() int {
				return ansiblemod.Run(context.Background(), tt.module,
					[]string{ansiblemod.Prefix + tt.module, argsPath},
					func(_ context.Context, got []string) error {
						argv = got
						return nil
					})
			})
			require.Equal(t, 0, code)
			require.NotEmpty(t, argv, "the module rendered no command line")

			target, flags, err := NewCargoshipCommand().Find(argv)
			require.NoError(t, err)
			require.Equal(t, tt.command, target.Name())
			require.NoError(t, target.ParseFlags(flags))

			// Spot-check the two flags that carry meaning rather than just a value: the dry run
			// check mode maps onto, and the confirmation a module has no terminal to give.
			if tt.dryRun {
				dryRun, err := target.Flags().GetBool(InstallDryRun)
				require.NoError(t, err)
				require.True(t, dryRun)
			}
			if tt.confirm != "" {
				confirm, err := target.Flags().GetBool(tt.confirm)
				require.NoError(t, err)
				require.True(t, confirm)
			}
		})
	}
}

func TestAnsibleModuleIgnoresTheOrdinaryCLI(t *testing.T) {
	t.Setenv(ansiblemod.EnvModule, "")
	_, ok := AnsibleModule(context.Background(), []string{"/usr/bin/cargoship", "version"})
	require.False(t, ok)
	_, ok = AnsibleModule(context.Background(), nil)
	require.False(t, ok)
}

// withStdout points file descriptor 1 at a throwaway file for the duration of fn, because the
// module under test writes its result to descriptor 1 directly and would otherwise write it into
// the test binary's own output.
func withStdout(t *testing.T, fn func() int) int {
	t.Helper()

	file, err := os.Create(filepath.Join(t.TempDir(), "stdout")) //nolint:gosec // a path this test just made
	require.NoError(t, err)
	defer file.Close() //nolint:errcheck

	fd, err := syscall.Dup(int(os.Stdout.Fd()))
	require.NoError(t, err)
	saved := os.NewFile(uintptr(fd), "saved-stdout")

	require.NoError(t, syscall.Dup2(int(file.Fd()), int(os.Stdout.Fd())))
	code := fn()
	require.NoError(t, syscall.Dup2(int(saved.Fd()), int(os.Stdout.Fd())))
	require.NoError(t, saved.Close())

	return code
}
