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
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/pkg/phase"
	"github.com/stretchr/testify/require"
)

func TestModuleName(t *testing.T) {
	t.Setenv(EnvModule, "")
	for _, tt := range []struct {
		name  string
		argv0 string
		want  string
		ok    bool
	}{
		{name: "plain binary", argv0: "/usr/bin/cargoship"},
		{name: "module symlink", argv0: "/tmp/ansible/cargoship_engine_config_sync", want: "engine_config_sync", ok: true},
		{name: "bare name", argv0: "cargoship_apply", want: "apply", ok: true},
		// Ansible runs a copy of the module file, under a name of its own making. This is the
		// name every real module run arrives under; the symlink name above is only ever seen by
		// an operator running the module by hand.
		{
			name:  "ansible copy",
			argv0: "/home/op/.ansible/tmp/ansible-tmp-1/AnsiballZ_cargoship_apply",
			want:  "apply", ok: true,
		},
		{name: "ansible copy of something else", argv0: "AnsiballZ_setup"},
		// A name that is nothing but the prefix selects no action, so it is not module mode.
		{name: "prefix only", argv0: "cargoship_"},
		// The prefix is not enough. This is the name the repository builds its own binary under
		// and the name every e2e suite runs, and it is an ordinary CLI invocation.
		{name: "the build name", argv0: "build/cargoship_linux_amd64"},
		// A name the binary does not answer as is the CLI too, including a misspelled module
		// file, which fails where it parses the arguments file as a command.
		{name: "misspelled module", argv0: "cargoship_engine_confg_sync"},
		{name: "unrelated", argv0: "kubectl"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ModuleName(tt.argv0)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestModuleNameFromEnvironment(t *testing.T) {
	t.Setenv(EnvModule, "engine_config_sync")
	got, ok := ModuleName("/usr/bin/cargoship")
	require.True(t, ok)
	require.Equal(t, "engine_config_sync", got)
}

// writeArgs writes a WANT_JSON arguments file and returns its path.
func writeArgs(t *testing.T, doc string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "args.json")
	require.NoError(t, os.WriteFile(path, []byte(doc), 0o600))
	return path
}

func TestReadArgsSplitsControlFromParameters(t *testing.T) {
	args, err := ReadArgs(writeArgs(t, `{
		"package": "./package.tar.zst",
		"_ansible_check_mode": true,
		"_ansible_verbosity": 2,
		"_ansible_something_added_later": "ignored"
	}`))
	require.NoError(t, err)
	require.True(t, args.Control.CheckMode)
	require.Equal(t, 2, args.Control.Verbosity)

	var p engineConfigSyncParams
	require.NoError(t, args.Params(&p))
	require.Equal(t, "./package.tar.zst", p.Package)
}

func TestReadArgsErrors(t *testing.T) {
	_, err := ReadArgs(filepath.Join(t.TempDir(), "absent.json"))
	require.ErrorContains(t, err, "unable to read the module arguments file")

	_, err = ReadArgs(writeArgs(t, "not json"))
	require.ErrorContains(t, err, "unable to parse the module arguments file")
}

func TestParamsNamesAnUnknownParameter(t *testing.T) {
	args, err := ReadArgs(writeArgs(t, `{"packge": "./package.tar.zst"}`))
	require.NoError(t, err)

	var p engineConfigSyncParams
	err = args.Params(&p)
	require.ErrorContains(t, err, `unknown parameter "packge"`)
	// The message has to be actionable on its own: there is no argument_spec and so no
	// ansible-doc to look the right name up in.
	require.ErrorContains(t, err, `"package"`)
}

func TestParamsRejectsAnUnknownKeyInsideTheInventory(t *testing.T) {
	args, err := ReadArgs(writeArgs(t, `{"inventory": {"groups": {}, "clustr": {}}}`))
	require.NoError(t, err)

	var p engineConfigSyncParams
	require.ErrorContains(t, args.Params(&p), "clustr")
}

func TestJSONFieldNamesFollowsEmbedding(t *testing.T) {
	type inner struct {
		A string `json:"a"`
	}
	type outer struct {
		inner
		B      string `json:"b,omitempty"`
		Hidden string `json:"-"`
		Bare   string
		skip   string //nolint:unused // unexported fields carry no JSON name
	}
	names := jsonFieldNames(reflect.TypeOf(&outer{}))
	require.Equal(t, map[string]bool{"a": true, "b": true, "Bare": true}, names)
}

// minimalInventory is the smallest inventory that translates and validates: one controller, named,
// with a load balancer.
const minimalInventory = `{
	"groups": {"controller": ["kc01"], "worker": ["kw01"]},
	"cluster": {"name": "bubbles", "loadbalancer": "bubbles-kc.test.com"}
}`

func TestBuildEngineConfigSyncArgsDefaults(t *testing.T) {
	p := engineConfigSyncParams{Package: "./package.tar.zst"}
	argv := buildEngineConfigSyncArgs(&p, "/tmp/inventory.yaml", Control{})
	require.Equal(t, []string{
		"engine-config-sync", "./package.tar.zst",
		"--config", "/tmp/inventory.yaml",
		"--confirm",
		"--no-color",
	}, argv)
}

func TestBuildEngineConfigSyncArgsCheckModeIsADryRun(t *testing.T) {
	p := engineConfigSyncParams{Package: "./package.tar.zst"}
	argv := buildEngineConfigSyncArgs(&p, "/tmp/inventory.yaml", Control{CheckMode: true})
	require.Contains(t, argv, "--dry-run")
}

func TestBuildEngineConfigSyncArgsOmitsUnsetOptionalFlags(t *testing.T) {
	// An unset boolean must not become --label-nodes=false: that would overrule whatever the
	// cargoship configuration file set, which is the opposite of saying nothing.
	p := engineConfigSyncParams{Package: "./package.tar.zst"}
	argv := strings.Join(buildEngineConfigSyncArgs(&p, "/tmp/inventory.yaml", Control{}), " ")
	require.NotContains(t, argv, "label-nodes")
	require.NotContains(t, argv, "update-kubeconfig")
	require.NotContains(t, argv, "concurrency")
}

func TestBuildEngineConfigSyncArgsFull(t *testing.T) {
	concurrency := 4
	labelNodes := false
	updateKubeconfig := true
	p := engineConfigSyncParams{
		Package:           "./package.tar.zst",
		Concurrency:       &concurrency,
		WorkConcurrency:   "25%",
		LabelNodes:        &labelNodes,
		UpdateKubeconfig:  &updateKubeconfig,
		Kubeconfig:        "/tmp/kubeconfig",
		Values:            []string{"/tmp/a.yaml", "/tmp/b.yaml"},
		Timeout:           "45m",
		common:            common{LogFormat: "json"},
		VaultPasswordFile: "/tmp/vault-pass",
		AgeIdentityFiles:  []string{"/tmp/age.key"},
	}
	argv := buildEngineConfigSyncArgs(&p, "/tmp/inventory.yaml", Control{Verbosity: 1})
	joined := strings.Join(argv, " ")

	require.Contains(t, joined, "--concurrency 4")
	require.Contains(t, joined, "--work-concurrency 25%")
	require.Contains(t, joined, "--label-nodes=false")
	require.Contains(t, joined, "--update-kubeconfig=true")
	require.Contains(t, joined, "--kubeconfig /tmp/kubeconfig")
	require.Contains(t, joined, "--values /tmp/a.yaml --values /tmp/b.yaml")
	require.Contains(t, joined, "--timeout 45m")
	require.Contains(t, joined, "--vault-password-file /tmp/vault-pass")
	require.Contains(t, joined, "--age-identity-file /tmp/age.key")
	require.Contains(t, joined, "--log-format json")
	// -v on the command line is a request for detail, so it becomes one.
	require.Contains(t, joined, "--log-level debug")
}

func TestLogLevelParameterBeatsVerbosity(t *testing.T) {
	require.Equal(t, "warn", logLevel("warn", Control{Verbosity: 3}))
	require.Equal(t, "debug", logLevel("", Control{Verbosity: 1}))
	require.Empty(t, logLevel("", Control{}))
}

// runModule drives Run with fd 1 pointed at a file, so that the stdout guard is exercised rather
// than bypassed, and returns the exit status with whatever landed on real standard output.
func runModule(t *testing.T, name string, argv []string, exec Exec) (int, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "stdout")
	file, err := os.Create(path) //nolint:gosec // a path this test just made
	require.NoError(t, err)
	defer file.Close() //nolint:errcheck

	saved, err := dupStdout()
	require.NoError(t, err)
	require.NoError(t, redirectStdout(file))
	code := Run(context.Background(), name, argv, exec)
	require.NoError(t, redirectStdout(saved))
	require.NoError(t, saved.Close())

	out, err := os.ReadFile(path) //nolint:gosec // a path this test just made
	require.NoError(t, err)
	return code, string(out)
}

// decodeOne asserts the module wrote exactly one JSON object and returns it.
func decodeOne(t *testing.T, out string) Response {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(out))
	var resp Response
	require.NoError(t, decoder.Decode(&resp))
	require.False(t, decoder.More(), "a module writes exactly one JSON object, and this wrote more")
	return resp
}

func TestRunReportsAnUnknownModule(t *testing.T) {
	code, out := runModule(t, "reticulate_splines", []string{"cargoship_reticulate_splines"}, nil)
	require.Equal(t, 0, code)
	resp := decodeOne(t, out)
	require.True(t, resp.Failed)
	require.Contains(t, resp.Msg, "cargoship_reticulate_splines")
	require.Contains(t, resp.Msg, "engine_config_sync")
}

func TestRunReportsAMissingArgumentsFile(t *testing.T) {
	code, out := runModule(t, "engine_config_sync", []string{"cargoship_engine_config_sync"}, nil)
	require.Equal(t, 0, code)
	resp := decodeOne(t, out)
	require.True(t, resp.Failed)
	require.Contains(t, resp.Msg, "WANT_JSON")
}

func TestRunRequiresPackageAndInventory(t *testing.T) {
	for _, tt := range []struct {
		name string
		args string
		want string
	}{
		{name: "no package", args: `{"inventory": ` + minimalInventory + `}`, want: `"package" parameter is required`},
		{name: "no inventory", args: `{"package": "./package.tar.zst"}`, want: `"inventory" parameter is required`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			argv := []string{"cargoship_engine_config_sync", writeArgs(t, tt.args)}
			code, out := runModule(t, "engine_config_sync", argv, nil)
			require.Equal(t, 0, code)
			resp := decodeOne(t, out)
			require.True(t, resp.Failed)
			require.Contains(t, resp.Msg, tt.want)
		})
	}
}

func TestRunTranslatesTheInventoryAndRunsTheCommand(t *testing.T) {
	var got []string
	exec := func(_ context.Context, argv []string) error {
		got = argv
		// Read the generated document while it still exists, which is what the command it
		// stands in for would have done.
		b, err := os.ReadFile(argv[3]) //nolint:gosec // the path this module just wrote
		require.NoError(t, err)
		require.Contains(t, string(b), "kind: ZarfCluster")
		require.Contains(t, string(b), "kc01")
		return nil
	}

	argv := []string{"cargoship_engine_config_sync", writeArgs(t,
		`{"package": "./package.tar.zst", "inventory": `+minimalInventory+`}`)}
	code, out := runModule(t, "engine_config_sync", argv, exec)
	require.Equal(t, 0, code)

	resp := decodeOne(t, out)
	require.False(t, resp.Failed)
	// This Exec runs no phases, so nothing reported and the module falls back to the
	// convention. The phase-reporting path is covered below.
	require.True(t, resp.Changed)
	require.Equal(t, SignalUnknown, resp.Cargoship.ChangedSignal)
	require.Equal(t, "engine_config_sync", resp.Cargoship.Module)
	require.Equal(t, "engine-config-sync", got[0])
	require.Equal(t, "cargoship", resp.Cargoship.Command[0])

	// A run that succeeded takes its temporary inventory away with it.
	require.False(t, resp.Cargoship.InventoryKept)
	require.NoFileExists(t, resp.Cargoship.InventoryPath)
}

func TestRunKeepsTheGeneratedInventoryWhenTheRunFails(t *testing.T) {
	exec := func(_ context.Context, _ []string) error {
		return errors.New("host kc01 refused the connection")
	}
	argv := []string{"cargoship_engine_config_sync", writeArgs(t,
		`{"package": "./package.tar.zst", "inventory": `+minimalInventory+`}`)}

	code, out := runModule(t, "engine_config_sync", argv, exec)
	require.Equal(t, 0, code, "a module reports failure in its result, not in its exit status")

	resp := decodeOne(t, out)
	require.True(t, resp.Failed)
	require.Contains(t, resp.Msg, "refused the connection")
	require.True(t, resp.Cargoship.InventoryKept)
	require.FileExists(t, resp.Cargoship.InventoryPath)
	require.NoError(t, os.RemoveAll(filepath.Dir(resp.Cargoship.InventoryPath)))
}

func TestRunWritesTheInventoryWhereTheOperatorAsked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inventory.yaml")
	argv := []string{"cargoship_engine_config_sync", writeArgs(t,
		`{"package": "./package.tar.zst", "inventory_path": "`+path+`", "inventory": `+minimalInventory+`}`)}

	code, out := runModule(t, "engine_config_sync", argv, func(context.Context, []string) error { return nil })
	require.Equal(t, 0, code)

	resp := decodeOne(t, out)
	require.False(t, resp.Failed)
	// A path the operator named is theirs, so it survives the run whatever the result.
	require.True(t, resp.Cargoship.InventoryKept)
	require.Equal(t, path, resp.Cargoship.InventoryPath)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestRunCheckModeIsReported(t *testing.T) {
	var got []string
	argv := []string{"cargoship_engine_config_sync", writeArgs(t,
		`{"package": "./package.tar.zst", "_ansible_check_mode": true, "inventory": `+minimalInventory+`}`)}

	code, out := runModule(t, "engine_config_sync", argv, func(_ context.Context, argv []string) error {
		got = argv
		return nil
	})
	require.Equal(t, 0, code)

	resp := decodeOne(t, out)
	require.True(t, resp.Cargoship.CheckMode)
	require.Contains(t, got, "--dry-run")
	require.Contains(t, resp.Msg, "check mode")
}

// TestRunGuardsStandardOutput is the contract the whole arrangement exists for: whatever the
// command being run prints, the module still writes exactly one JSON object.
func TestRunGuardsStandardOutput(t *testing.T) {
	exec := func(context.Context, []string) error {
		os.Stdout.WriteString("a phase that prints, which no phase should\n") //nolint:errcheck
		return nil
	}
	argv := []string{"cargoship_engine_config_sync", writeArgs(t,
		`{"package": "./package.tar.zst", "inventory": `+minimalInventory+`}`)}

	_, out := runModule(t, "engine_config_sync", argv, exec)
	require.NotContains(t, out, "no phase should")
	resp := decodeOne(t, out)
	require.False(t, resp.Failed)
}

func TestResponseEmitsOneObjectPerLine(t *testing.T) {
	var sb strings.Builder
	resp := &Response{Changed: true, Msg: "done"}
	require.NoError(t, resp.emit(&sb))

	out := sb.String()
	// One object, one line, and nothing the module did not set: a field emitted empty is a
	// field a playbook can write a conditional against and be misled by.
	require.Equal(t, 1, strings.Count(out, "\n"))
	require.True(t, strings.HasSuffix(out, "\n"))
	require.JSONEq(t, `{"changed":true,"msg":"done"}`, strings.TrimSpace(out))
}

// syncedPhase stands in for EngineConfigSyncHosts: it is the kind of phase that says whether it
// changed the fleet.
type syncedPhase struct {
	changed bool
	skip    bool
}

func (p *syncedPhase) Title() string       { return "Sync Registry Config Controller" }
func (p *syncedPhase) Explanation() string { return "test phase" }
func (p *syncedPhase) ShouldRun() bool     { return !p.skip }
func (p *syncedPhase) Changed() bool       { return p.changed }

func (p *syncedPhase) Run(context.Context) error { return nil }

// silentPhase declares nothing about what it changed, which is what most of the phase tree still
// does. It is what makes a signal partial.
type silentPhase struct{}

func (p *silentPhase) Title() string             { return "Connect" }
func (p *silentPhase) Explanation() string       { return "test phase" }
func (p *silentPhase) Run(context.Context) error { return nil }

// runPhases is an Exec that runs a real phase manager on the context the module handed it, which
// is the whole path the changed signal travels. A dry run is selected the way the real command
// selects one, by the flag the module rendered, so check mode is proved end to end.
func runPhases(phases ...phase.Phase) Exec {
	return func(ctx context.Context, argv []string) error {
		m := phase.Manager{Config: &cluster.ZarfCluster{Spec: cluster.ZarfClusterSpec{}}}
		for _, a := range argv {
			if a == "--dry-run" {
				m.DryRun = true
			}
		}
		m.SetPhases(phases)
		return m.Run(ctx)
	}
}

func TestRunReportsWhatThePhasesChanged(t *testing.T) {
	argv := []string{"cargoship_engine_config_sync", writeArgs(t,
		`{"package": "./package.tar.zst", "inventory": `+minimalInventory+`}`)}

	code, out := runModule(t, "engine_config_sync", argv,
		runPhases(&silentPhase{}, &syncedPhase{changed: true}))
	require.Equal(t, 0, code)

	resp := decodeOne(t, out)
	require.False(t, resp.Failed)
	require.True(t, resp.Changed)
	require.Equal(t, SignalPartial, resp.Cargoship.ChangedSignal)
	require.Equal(t, []string{"Connect"}, resp.Cargoship.ChangedUndeclared)
	require.Equal(t, []string{"Connect", "Sync Registry Config Controller"}, resp.Cargoship.PhasesRan)
	require.Empty(t, resp.Cargoship.PhasesPlanned)
}

// TestRunReportsNoChangeWhenTheFleetIsAlreadySynced is the result the whole of step four exists
// to make possible. It used to be reported as changed regardless, which made an Ansible handler
// on this task fire on every run.
func TestRunReportsNoChangeWhenTheFleetIsAlreadySynced(t *testing.T) {
	argv := []string{"cargoship_engine_config_sync", writeArgs(t,
		`{"package": "./package.tar.zst", "inventory": `+minimalInventory+`}`)}

	code, out := runModule(t, "engine_config_sync", argv,
		runPhases(&syncedPhase{skip: true}))
	require.Equal(t, 0, code)

	resp := decodeOne(t, out)
	require.False(t, resp.Failed)
	require.False(t, resp.Changed)
	require.Equal(t, SignalComplete, resp.Cargoship.ChangedSignal)
	require.Empty(t, resp.Cargoship.ChangedUndeclared)
}

// TestRunCheckModeReportsOutstandingWork pins what changed means under check mode: not "this run
// changed something", which is never true of a dry run, but "a real run would".
func TestRunCheckModeReportsOutstandingWork(t *testing.T) {
	for _, tc := range []struct {
		name    string
		phase   *syncedPhase
		changed bool
		planned []string
	}{
		{
			name:    "work outstanding",
			phase:   &syncedPhase{},
			changed: true,
			planned: []string{"Sync Registry Config Controller"},
		},
		{
			name:  "nothing to do",
			phase: &syncedPhase{skip: true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			argv := []string{"cargoship_engine_config_sync", writeArgs(t,
				`{"_ansible_check_mode": true, "package": "./package.tar.zst", "inventory": `+minimalInventory+`}`)}

			code, out := runModule(t, "engine_config_sync", argv, runPhases(tc.phase))
			require.Equal(t, 0, code)

			resp := decodeOne(t, out)
			require.False(t, resp.Failed)
			require.True(t, resp.Cargoship.CheckMode)
			require.Equal(t, tc.changed, resp.Changed)
			require.Equal(t, tc.planned, resp.Cargoship.PhasesPlanned)
		})
	}
}

func TestBuildApplyArgsFull(t *testing.T) {
	concurrency := 4
	yes, no := true, false
	p := applyParams{
		Package:             "./package.tar.zst",
		Concurrency:         &concurrency,
		WorkConcurrency:     "25%",
		hostUpdates:         hostUpdates{Hosts: &yes, Firewall: &no, FAPolicyd: &yes},
		LabelNodes:          &yes,
		AllowUnmanagedNodes: &yes,
		UpdateKubeconfig:    &yes,
		Kubeconfig:          "/tmp/kubeconfig",
		Values:              []string{"/tmp/a.yaml"},
		Timeout:             "45m",
		VaultPasswordFile:   "/tmp/vault-pass",
		AgeIdentityFiles:    []string{"/tmp/age.key"},
		verification:        verification{PublicKey: "/tmp/cosign.pub", Verify: "always"},
	}
	argv := buildApplyArgs(&p, "/tmp/inventory.yaml", Control{})
	joined := strings.Join(argv, " ")

	require.Equal(t, "apply", argv[0])
	require.Equal(t, "./package.tar.zst", argv[1])
	require.Contains(t, joined, "--config /tmp/inventory.yaml")
	require.Contains(t, joined, "--confirm")
	require.Contains(t, joined, "--hosts=true")
	require.Contains(t, joined, "--firewall=false")
	require.Contains(t, joined, "--fapolicyd=true")
	require.Contains(t, joined, "--allow-unmanaged-nodes=true")
	require.Contains(t, joined, "--key /tmp/cosign.pub")
	require.Contains(t, joined, "--verify always")
	require.NotContains(t, joined, "--dry-run")
}

func TestBuildPrepareArgsDefaults(t *testing.T) {
	p := prepareParams{Package: "./package.tar.zst"}
	require.Equal(t, []string{
		"prepare", "./package.tar.zst",
		"--config", "/tmp/inventory.yaml",
		"--confirm",
		"--no-color",
	}, buildPrepareArgs(&p, "/tmp/inventory.yaml", Control{}))
}

// TestPrepareRejectsKeyMaterialParameters pins the parameter surfaces to the commands they render.
// `cargoship prepare` reads no encrypted value, so a playbook that hands it a vault password has
// misunderstood something and is told which parameter.
func TestPrepareRejectsKeyMaterialParameters(t *testing.T) {
	args, err := ReadArgs(writeArgs(t, `{"package": "p", "vault_password_file": "/tmp/vault-pass"}`))
	require.NoError(t, err)

	var p prepareParams
	require.ErrorContains(t, args.Params(&p), "vault_password_file")
}

func TestBuildResetArgs(t *testing.T) {
	no := false
	p := resetParams{Distro: "rke2", hostUpdates: hostUpdates{Firewall: &no}}
	joined := strings.Join(buildResetArgs(&p, "/tmp/inventory.yaml", Control{CheckMode: true}), " ")

	// Reset takes no package: the positional argument is absent and the distro is named.
	require.True(t, strings.HasPrefix(joined, "reset --config /tmp/inventory.yaml"))
	require.Contains(t, joined, "--distro rke2")
	require.Contains(t, joined, "--firewall=false")
	require.Contains(t, joined, "--dry-run")
}

func TestBuildKubeConfigArgs(t *testing.T) {
	p := kubeConfigParams{Distro: "k3s", Kubeconfig: "/tmp/kubeconfig"}
	argv := buildKubeConfigArgs(&p, "/tmp/inventory.yaml", Control{})
	require.Equal(t, []string{
		"kube-config",
		"--config", "/tmp/inventory.yaml",
		"--no-color",
		"--distro", "k3s",
		"--kubeconfig", "/tmp/kubeconfig",
	}, argv)
}

// TestRunKubeConfigSkipsUnderCheckMode is what an action without a dry run owes an operator who
// asked for one: the task is reported skipped and nothing is run. Running anyway would change the
// management node during a run that was asked only to describe itself.
func TestRunKubeConfigSkipsUnderCheckMode(t *testing.T) {
	ran := false
	argv := []string{"cargoship_kube_config", writeArgs(t,
		`{"_ansible_check_mode": true, "inventory": `+minimalInventory+`}`)}

	code, out := runModule(t, "kube_config", argv, func(context.Context, []string) error {
		ran = true
		return nil
	})
	require.Equal(t, 0, code)
	require.False(t, ran)

	resp := decodeOne(t, out)
	require.True(t, resp.Skipped)
	require.False(t, resp.Changed)
	require.False(t, resp.Failed)
	require.Contains(t, resp.Msg, "no dry run")
	require.Empty(t, resp.Cargoship.InventoryPath, "a skipped task writes nothing")
}

func TestRunKubeConfigRunsOutsideCheckMode(t *testing.T) {
	var got []string
	argv := []string{"cargoship_kube_config", writeArgs(t,
		`{"distro": "rke2", "inventory": `+minimalInventory+`}`)}

	code, out := runModule(t, "kube_config", argv, func(_ context.Context, argv []string) error {
		got = argv
		return nil
	})
	require.Equal(t, 0, code)

	resp := decodeOne(t, out)
	require.False(t, resp.Skipped)
	require.False(t, resp.Failed)
	require.Equal(t, "kube-config", got[0])
	require.NotContains(t, got, "--confirm")
}

func TestModulesAreTheFleetActions(t *testing.T) {
	require.Equal(t,
		[]string{"apply", "engine_config_sync", "kube_config", "prepare", "reset"},
		Modules())
}
