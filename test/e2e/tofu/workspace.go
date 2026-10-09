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

package tofu

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

const (
	// providerMirrorDir and providerMirrorPath are where `mage release:tofuProviderDev` puts the
	// provider, split the way a filesystem_mirror reads it: the directory a `.tofurc` points at,
	// then the source address below it. The registry hostname is part of the address even for a
	// provider no registry serves -- a mirror redirects an address rather than replacing it.
	providerMirrorDir  = "build/tofu-provider/mirror"
	providerMirrorPath = "registry.opentofu.org/colonel-byte/cargoship"

	// providerSource is the address the module's required_providers names.
	providerSource = "registry.opentofu.org/colonel-byte/cargoship"

	// moduleDir is the module under test. The suite drives the module rather than writing a
	// bare resource, because the module is what a practitioner actually consumes: its variable
	// types have to accept what the provider's schema wants, and a type that no longer converts
	// is a failure nothing else here would see.
	moduleDir = "example/terragrunt/terraform/modules/cluster"

	// rootModuleFixture is the root module the suite runs. It is a file rather than a string
	// built in Go, per test/e2e/AGENTS.md: it is HCL a reviewer reads as HCL, and the only part
	// of this workspace that varies per run is data, which goes in the generated tfvars.
	rootModuleFixture = "test/e2e/tofu/testdata/main.tf"
)

// tofuBinaryEnvVar names the binary to run, for a machine that has it under another name.
const tofuBinaryEnvVar = "CARGOSHIP_E2E_TOFU_BINARY"

func tofuBinary() string {
	if name := os.Getenv(tofuBinaryEnvVar); name != "" {
		return name
	}
	return "tofu"
}

// hostPlatform is the directory a mirror keeps this machine's build in.
func hostPlatform() string {
	return fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH)
}

// providerBinaryName is the file OpenTofu looks for inside that directory. The name is not ours
// to choose.
func providerBinaryName(version string) string {
	name := fmt.Sprintf("terraform-provider-cargoship_v%s", version)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// workspace is one OpenTofu working directory: the root module, the CLI configuration pointing
// at the local mirror, and the state the run accumulates.
//
// It lives in a temp directory rather than beside the module, so a run leaves no state file in
// the repository and two runs cannot share a lock.
type workspace struct {
	dir     string
	cliFile string
}

// newWorkspace assembles that directory.
//
// The module arrives as a symlink rather than a copy, and the root module names it as `./module`.
// A local module source is resolved relative to the file that names it, so a copied root module
// could not reach back into the repository without a path that depends on where the temp
// directory landed; a symlink keeps the source stable and the module itself unmodified.
func newWorkspace(t *testing.T, mirror string) *workspace {
	t.Helper()

	dir := t.TempDir()

	body, err := os.ReadFile(filepath.Join(rootDir, rootModuleFixture))
	if err != nil {
		t.Fatalf("reading the root module fixture %s: %v", rootModuleFixture, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), body, 0o600); err != nil {
		t.Fatalf("writing the root module: %v", err)
	}
	if err := os.Symlink(filepath.Join(rootDir, moduleDir), filepath.Join(dir, "module")); err != nil {
		t.Fatalf("linking the module under test: %v", err)
	}

	w := &workspace{
		dir:     dir,
		cliFile: filepath.Join(dir, "tofurc"),
	}
	w.writeCLIConfig(t, mirror)
	return w
}

// writeCLIConfig writes the CLI configuration that keeps the run offline.
//
// The `direct` block with the matching exclusion is what stops OpenTofu reaching for the registry
// for this provider while leaving every other provider alone -- which matters because an
// air-gapped machine is the environment cargoship is for, and a suite that silently needed the
// network would pass only where the network is.
//
// It is written here rather than kept as a fixture because the mirror path is absolute and known
// only at run time, and a fixture holding a placeholder would be a template rather than a
// document.
func (w *workspace) writeCLIConfig(t *testing.T, mirror string) {
	t.Helper()

	cfg := fmt.Sprintf(`provider_installation {
  filesystem_mirror {
    path    = %q
    include = ["%s"]
  }
  direct {
    exclude = ["%s"]
  }
}
`, mirror, providerSource, providerSource)

	if err := os.WriteFile(w.cliFile, []byte(cfg), 0o600); err != nil {
		t.Fatalf("writing the OpenTofu CLI configuration: %v", err)
	}
}

// writeVars writes the generated half of the configuration: the fleet, the package and the
// settings a step is varying. It is JSON rather than HCL because it is data a machine produced,
// and `terraform.tfvars.json` is read without being named on the command line.
func (w *workspace) writeVars(t *testing.T, vars map[string]any) {
	t.Helper()

	body, err := json.MarshalIndent(vars, "", "  ")
	if err != nil {
		t.Fatalf("encoding the tofu variables: %v", err)
	}
	if err := os.WriteFile(filepath.Join(w.dir, "terraform.tfvars.json"), body, 0o600); err != nil {
		t.Fatalf("writing the tofu variables: %v", err)
	}
}

// run runs one tofu command in the workspace and fails the test with its output if it exits
// non-zero. The output is returned either way, because a step that fails is read for what the
// provider said rather than for the exit code.
func (w *workspace) run(t *testing.T, args ...string) string {
	t.Helper()
	out, err := w.tryRun(t, args...)
	if err != nil {
		t.Fatalf("tofu %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// tryRun is run for a step that expects to read the failure rather than stop at it.
func (w *workspace) tryRun(t *testing.T, args ...string) (string, error) {
	t.Helper()

	cmd := exec.Command(tofuBinary(), args...) //nolint:gosec // the arguments are this suite's own
	cmd.Dir = w.dir
	cmd.Env = append(os.Environ(),
		"TF_CLI_CONFIG_FILE="+w.cliFile,
		// TF_IN_AUTOMATION drops the "run tofu plan next" advice from the output, and
		// TF_INPUT stops a missing variable from waiting on a prompt no test can answer.
		"TF_IN_AUTOMATION=1",
		"TF_INPUT=0",
		"NO_COLOR=1",
	)

	out, err := cmd.CombinedOutput()
	plain := ansi.ReplaceAllString(string(out), "")
	t.Logf("tofu %s\n%s", strings.Join(args, " "), plain)
	return plain, err
}

// ansi matches the escape sequences OpenTofu colours its output with.
//
// NO_COLOR and TF_IN_AUTOMATION do not turn them off -- the `-no-color` flag does, but it is not
// accepted by every subcommand, so the output is stripped here instead. It matters for more than
// legibility: an assertion over coloured output is matching against escape codes interleaved with
// the words, which fails for reasons that have nothing to do with what the command reported.
var ansi = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// outputValues is the root module's outputs, as `tofu output -json` reports them.
type outputValues map[string]struct {
	Value     any  `json:"value"`
	Sensitive bool `json:"sensitive"`
}

func (w *workspace) outputs(t *testing.T) outputValues {
	t.Helper()

	out := w.run(t, "output", "-json")
	var values outputValues
	if err := json.Unmarshal([]byte(out), &values); err != nil {
		t.Fatalf("decoding the tofu outputs: %v\n%s", err, out)
	}
	return values
}

// stringOutput reads one output that is expected to be a non-empty string.
func (w *workspace) stringOutput(t *testing.T, name string) string {
	t.Helper()

	value, ok := w.outputs(t)[name]
	if !ok {
		t.Fatalf("the root module has no %q output", name)
	}
	text, ok := value.Value.(string)
	if !ok {
		t.Fatalf("the %q output is a %T rather than a string", name, value.Value)
	}
	if text == "" {
		t.Fatalf("the %q output is empty", name)
	}
	return text
}

// clusterState is the `cargoship_cluster` resource as state holds it, which is where the
// attributes an apply computed can be read back: the per-host `removed` marker, the engine the
// package carried, and the nodes the phases reported.
type clusterState struct {
	Distro        string `json:"distro"`
	EngineVersion string `json:"engine_version"`
	Hosts         map[string]struct {
		Address  string `json:"address"`
		Role     string `json:"role"`
		State    string `json:"state"`
		Removed  bool   `json:"removed"`
		Hostname string `json:"hostname"`
	} `json:"hosts"`
	Nodes []struct {
		Address       string `json:"address"`
		Hostname      string `json:"hostname"`
		EngineVersion string `json:"engine_version"`
		Role          string `json:"role"`
	} `json:"nodes"`
}

// stateShow reads that resource out of `tofu show -json`.
//
// The resource is addressed by walking the state's module tree rather than by string matching on
// `module.cluster.cargoship_cluster.this`, so a root module that renames the module call stays
// readable here.
func (w *workspace) clusterFromState(t *testing.T) clusterState {
	t.Helper()

	out := w.run(t, "show", "-json")

	var show struct {
		Values struct {
			RootModule struct {
				ChildModules []struct {
					Resources []struct {
						Type   string          `json:"type"`
						Values json.RawMessage `json:"values"`
					} `json:"resources"`
				} `json:"child_modules"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if err := json.Unmarshal([]byte(out), &show); err != nil {
		t.Fatalf("decoding the tofu state: %v", err)
	}

	for _, module := range show.Values.RootModule.ChildModules {
		for _, resource := range module.Resources {
			if resource.Type != "cargoship_cluster" {
				continue
			}
			var cluster clusterState
			if err := json.Unmarshal(resource.Values, &cluster); err != nil {
				t.Fatalf("decoding the cargoship_cluster resource: %v", err)
			}
			return cluster
		}
	}
	t.Fatal("state holds no cargoship_cluster resource")
	return clusterState{}
}

// stateIsEmpty reports whether state holds nothing, which is both what a finished destroy leaves
// and what a walk that failed before its first apply never wrote.
//
// The two are told apart by the file rather than by the command: `tofu state list` exits non-zero
// with "No state file was found" when there is none, and running it anyway would report the
// cleanup as the failure instead of whatever actually failed.
func (w *workspace) stateIsEmpty(t *testing.T) bool {
	t.Helper()

	if _, err := os.Stat(filepath.Join(w.dir, "terraform.tfstate")); err != nil {
		return true
	}
	return strings.TrimSpace(w.run(t, "state", "list")) == ""
}
