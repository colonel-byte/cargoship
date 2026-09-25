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

package ansiblemod

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/internal/ansibleinv"
	goyaml "github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

// documentationBlock pulls the DOCUMENTATION literal out of an action plugin, the same way
// magefiles/gen-docs.go reads it and ansibleinv's plugin test reads the variable allowlist.
var documentationBlock = regexp.MustCompile(`(?sm)^DOCUMENTATION = r"""\n(.*?)\n"""$`)

// pluginParameters is the part of a DOCUMENTATION block this test reads. Order is not checked here:
// it is the row order of the generated page, which is the generator's business, not the contract's.
type pluginParameters struct {
	Options map[string]struct {
		// CLIFlag is the flag the parameter renders, or "None" when it renders none.
		CLIFlag string `yaml:"cli_flag"`
	} `yaml:"options"`
}

// pluginOnlyParameter is documented but has no Go field, because the action plugin consumes it and
// the module never sees it. See plugins/plugin_utils/projection.py.
const pluginOnlyParameter = "parameters"

// moduleOwnFlags are the flags a module adds for itself rather than for any parameter: the
// generated inventory, the confirmation there is no terminal to give, the colour Ansible would
// capture as noise, and check mode's dry run. No documented parameter controls them, so the
// every-flag-is-documented direction below has to let them through.
var moduleOwnFlags = map[string]bool{
	flagConfig:  true,
	flagConfirm: true,
	flagNoColor: true,
	flagDryRun:  true,
}

// TestActionPluginDocsMatchModuleParams is what stops a module's documented interface drifting from
// the one it has.
//
// The documentation is in YAML inside a Python file and the interface is a Go struct, so nothing
// but a test relates them. Two ways they drift, both of which reach an operator as a parameter that
// is documented and rejected, or accepted and undocumented:
//
//   - a parameter added to, removed from, or renamed in the Go struct without the DOCUMENTATION
//     block following it. Checked against jsonFieldNames, which is what Args.Params itself decodes
//     through, so the test is reading the same list the module accepts.
//   - a parameter whose documented cli_flag is not the flag the module renders. A flag cannot be
//     read off a struct tag -- it exists only as a statement inside buildApplyArgs and its
//     siblings -- so each case populates every parameter, renders the command line, and checks the
//     documented flags against the ones that came out.
func TestActionPluginDocsMatchModuleParams(t *testing.T) {
	control := Control{}
	const inventory = "/run/cargoship/inventory.yaml"

	for _, tt := range []struct {
		module string
		params any
		argv   []string
	}{
		{
			module: "cargoship_apply",
			params: &applyParams{},
			argv:   buildApplyArgs(fullApplyParams(), inventory, control),
		},
		{
			module: "cargoship_prepare",
			params: &prepareParams{},
			argv:   buildPrepareArgs(fullPrepareParams(), inventory, control),
		},
		{
			module: "cargoship_engine_config_sync",
			params: &engineConfigSyncParams{},
			argv:   buildEngineConfigSyncArgs(fullEngineConfigSyncParams(), inventory, control),
		},
		{
			module: "cargoship_reset",
			params: &resetParams{},
			argv:   buildResetArgs(fullResetParams(), inventory, control),
		},
		{
			module: "cargoship_kube_config",
			params: &kubeConfigParams{},
			argv:   buildKubeConfigArgs(fullKubeConfigParams(), inventory, control),
		},
	} {
		t.Run(tt.module, func(t *testing.T) {
			documented := documentedOptions(t, tt.module)

			t.Run("every parameter is documented", func(t *testing.T) {
				names := make([]string, 0, len(documented))
				for name := range documented {
					if name == pluginOnlyParameter {
						continue
					}
					names = append(names, name)
				}
				sort.Strings(names)

				require.Equal(t, sortedKeys(jsonFieldNames(reflect.TypeOf(tt.params))), names)
			})

			t.Run("every documented flag is rendered", func(t *testing.T) {
				rendered := renderedFlags(tt.argv)
				for name, option := range documented {
					flag, renders := strings.CutPrefix(option.CLIFlag, "--")
					if !renders {
						// "None", the documented spelling for a parameter that renders no flag.
						require.Equal(t, "None", option.CLIFlag,
							"parameter %s documents cli_flag %q, which is neither a flag nor None",
							name, option.CLIFlag)
						continue
					}
					require.Contains(t, rendered, flag,
						"parameter %s documents cli_flag %q, which the module does not render", name, option.CLIFlag)
				}
			})

			t.Run("every rendered flag is documented", func(t *testing.T) {
				flags := map[string]bool{}
				for _, option := range documented {
					if flag, renders := strings.CutPrefix(option.CLIFlag, "--"); renders {
						flags[flag] = true
					}
				}
				for _, flag := range renderedFlags(tt.argv) {
					if moduleOwnFlags[flag] {
						continue
					}
					require.True(t, flags[flag],
						"the module renders --%s, which no documented parameter names", flag)
				}
			})
		})
	}
}

// documentedOptions reads one action plugin's documented parameters.
func documentedOptions(t *testing.T, module string) map[string]struct {
	CLIFlag string `yaml:"cli_flag"`
} {
	t.Helper()

	path := filepath.Join(collectionRoot, "plugins", "action", module+".py")
	source, err := os.ReadFile(path)
	require.NoError(t, err)

	block := documentationBlock.FindSubmatch(source)
	require.NotNil(t, block, `%s has no DOCUMENTATION = r""" block`, path)

	var parsed pluginParameters
	require.NoError(t, goyaml.Unmarshal(block[1], &parsed), "the DOCUMENTATION block in %s", path)
	require.NotEmpty(t, parsed.Options, "%s documents no options", path)

	return parsed.Options
}

// renderedFlags returns the flag names in a command line, sorted and without duplicates. A boolean
// flag is written --name=value and a repeated one appears once per value, so neither the value nor
// the count is what this compares.
func renderedFlags(argv []string) []string {
	seen := map[string]bool{}
	for _, arg := range argv {
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		name, _, _ := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		seen[name] = true
	}
	return sortedKeys(seen)
}

// The parameter sets below are populated in full, because a flag only appears in the rendered
// command line when its parameter is set. A field left at its zero value would be read as a flag
// the module does not render, and the documentation for it as drift.
//
// Every value is arbitrary except in one respect: none of them starts with a dash, or renderedFlags
// would count it as a flag of its own.

func fullCommon() common {
	logFile := true
	return common{
		Inventory:         &ansibleinv.Request{},
		InventoryPath:     "/run/cargoship/inventory.yaml",
		LogLevel:          "debug",
		LogFormat:         "json",
		LogFile:           &logFile,
		AgeRecipients:     []string{"age1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsxxxxxx"},
		AgeRecipientFiles: []string{"/etc/cargoship/recipients.txt"},
	}
}

func fullHostUpdates() hostUpdates {
	yes := true
	return hostUpdates{Hosts: &yes, Firewall: &yes, FAPolicyd: &yes}
}

func fullVerification() verification {
	return verification{PublicKey: "/etc/cargoship/cosign.pub", Verify: "always"}
}

func fullApplyParams() *applyParams {
	concurrency, yes := 4, true
	return &applyParams{
		common:              fullCommon(),
		hostUpdates:         fullHostUpdates(),
		verification:        fullVerification(),
		Package:             "/srv/staging/rke2.tar.zst",
		Concurrency:         &concurrency,
		WorkConcurrency:     "50%",
		LabelNodes:          &yes,
		AllowUnmanagedNodes: &yes,
		UpdateKubeconfig:    &yes,
		Kubeconfig:          "/srv/staging/cluster.kubeconfig",
		Values:              []string{"/srv/staging/values.yaml"},
		Timeout:             "45m",
		VaultPasswordFile:   "/srv/staging/vault-pass",
		AgeIdentityFiles:    []string{"/srv/staging/age.key"},
	}
}

func fullPrepareParams() *prepareParams {
	concurrency := 4
	return &prepareParams{
		common:          fullCommon(),
		hostUpdates:     fullHostUpdates(),
		verification:    fullVerification(),
		Package:         "/srv/staging/rke2.tar.zst",
		Concurrency:     &concurrency,
		WorkConcurrency: "50%",
		Values:          []string{"/srv/staging/values.yaml"},
		Timeout:         "45m",
	}
}

func fullEngineConfigSyncParams() *engineConfigSyncParams {
	concurrency, yes := 4, true
	return &engineConfigSyncParams{
		common:            fullCommon(),
		verification:      fullVerification(),
		Package:           "/srv/staging/rke2.tar.zst",
		Concurrency:       &concurrency,
		WorkConcurrency:   "50%",
		LabelNodes:        &yes,
		UpdateKubeconfig:  &yes,
		Kubeconfig:        "/srv/staging/cluster.kubeconfig",
		Values:            []string{"/srv/staging/values.yaml"},
		Timeout:           "45m",
		VaultPasswordFile: "/srv/staging/vault-pass",
		AgeIdentityFiles:  []string{"/srv/staging/age.key"},
	}
}

func fullResetParams() *resetParams {
	concurrency := 4
	return &resetParams{
		common:          fullCommon(),
		hostUpdates:     fullHostUpdates(),
		Distro:          "rke2",
		Concurrency:     &concurrency,
		WorkConcurrency: "50%",
	}
}

func fullKubeConfigParams() *kubeConfigParams {
	return &kubeConfigParams{
		common:     fullCommon(),
		Distro:     "rke2",
		Kubeconfig: "/srv/staging/cluster.kubeconfig",
	}
}
