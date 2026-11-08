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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/config"
)

const renderTemplate = `apiVersion: colonel-byte.dev/v1alpha1
kind: Distro
metadata:
  name: {{ .name }}
  version: {{ .version }}
spec:
  type: {{ .name }}
  version: {{ .version }}
  config:
    os: {}
`

// writeRenderInputs writes a definition template and a values file, and returns their paths
// along with the directory a render should write into.
func writeRenderInputs(t *testing.T, values string) (tmpl, vals, out string) {
	t.Helper()

	dir := t.TempDir()
	tmpl = filepath.Join(dir, config.DistroYAML+".tmpl")
	if err := os.WriteFile(tmpl, []byte(renderTemplate), 0o600); err != nil {
		t.Fatalf("unable to write the template: %v", err)
	}
	vals = filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(vals, []byte(values), 0o600); err != nil {
		t.Fatalf("unable to write the values: %v", err)
	}
	return tmpl, vals, filepath.Join(dir, "out")
}

func TestDevRenderWritesADefinition(t *testing.T) {
	tmpl, vals, out := writeRenderInputs(t, "name: k3s\nversion: 1.36.4-k3s1\n")

	cmd := newDevRenderCommand()
	cmd.SetOut(&strings.Builder{})
	cmd.SetArgs([]string{"--template", tmpl, "--values", vals, "--out", out})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("render returned %v", err)
	}

	b, err := os.ReadFile(filepath.Join(out, config.DistroYAML))
	if err != nil {
		t.Fatalf("unable to read the rendered definition: %v", err)
	}
	if !strings.Contains(string(b), "version: 1.36.4-k3s1") {
		t.Fatalf("the rendered definition does not carry the version:\n%s", b)
	}
}

// A definition that fails the schema check is not written at all, so a failed render cannot be
// mistaken for a finished one.
func TestDevRenderWritesNothingWhenValidationFails(t *testing.T) {
	tmpl, vals, out := writeRenderInputs(t, "name: k3s\nversion: []\n")

	cmd := newDevRenderCommand()
	cmd.SetOut(&strings.Builder{})
	cmd.SetArgs([]string{"--template", tmpl, "--values", vals, "--out", out})
	if err := cmd.Execute(); err == nil {
		t.Fatal("render accepted a definition the package schema rejects")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("the output directory exists after a failed render: %v", err)
	}
}

func TestDevRenderNoValidateWritesAnyway(t *testing.T) {
	tmpl, vals, out := writeRenderInputs(t, "name: k3s\nversion: []\n")

	cmd := newDevRenderCommand()
	cmd.SetOut(&strings.Builder{})
	cmd.SetArgs([]string{"--template", tmpl, "--values", vals, "--out", out, "--no-validate"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("render returned %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, config.DistroYAML)); err != nil {
		t.Fatalf("--no-validate did not write the definition: %v", err)
	}
}

// The hazard --out's default creates: a template named for what it renders to.
func TestDevRenderRefusesToOverwriteItsTemplate(t *testing.T) {
	dir := t.TempDir()
	tmpl := filepath.Join(dir, config.DistroYAML)
	if err := os.WriteFile(tmpl, []byte(renderTemplate), 0o600); err != nil {
		t.Fatalf("unable to write the template: %v", err)
	}
	vals := filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(vals, []byte("name: k3s\nversion: 1.36.4-k3s1\n"), 0o600); err != nil {
		t.Fatalf("unable to write the values: %v", err)
	}

	cmd := newDevRenderCommand()
	cmd.SetOut(&strings.Builder{})
	cmd.SetArgs([]string{"--template", tmpl, "--values", vals, "--out", dir})
	if err := cmd.Execute(); err == nil {
		t.Fatal("render wrote its own output over the template")
	}

	b, err := os.ReadFile(tmpl)
	if err != nil {
		t.Fatalf("unable to read the template back: %v", err)
	}
	if string(b) != renderTemplate {
		t.Fatalf("the template was modified:\n%s", b)
	}
}

// A template not named distro.yaml is not a package definition, so nothing validates it -
// which the run has to say, or an operator reads the silence as a pass.
func TestDevRenderSaysWhenNothingWasValidated(t *testing.T) {
	_, vals, out := writeRenderInputs(t, "name: k3s\nversion: 1.36.4-k3s1\n")

	other := filepath.Join(filepath.Dir(vals), "notes.yaml.tmpl")
	if err := os.WriteFile(other, []byte("version: {{ .version }}\n"), 0o600); err != nil {
		t.Fatalf("unable to write the template: %v", err)
	}

	stdout := &strings.Builder{}
	cmd := newDevRenderCommand()
	cmd.SetOut(stdout)
	cmd.SetArgs([]string{"--template", other, "--values", vals, "--out", out})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("render returned %v", err)
	}
	if !strings.Contains(stdout.String(), "nothing was checked against the package schema") {
		t.Fatalf("render did not report that it validated nothing:\n%s", stdout.String())
	}
}

func TestDevRenderMergesValuesFilesInOrder(t *testing.T) {
	tmpl, vals, out := writeRenderInputs(t, "name: k3s\nversion: 1.36.4-k3s1\n")

	override := filepath.Join(filepath.Dir(vals), "override.yaml")
	if err := os.WriteFile(override, []byte("version: 1.37.1-k3s1\n"), 0o600); err != nil {
		t.Fatalf("unable to write the override: %v", err)
	}

	cmd := newDevRenderCommand()
	cmd.SetOut(&strings.Builder{})
	cmd.SetArgs([]string{"-t", tmpl, "--values", vals, "--values", override, "-o", out})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("render returned %v", err)
	}

	b, err := os.ReadFile(filepath.Join(out, config.DistroYAML))
	if err != nil {
		t.Fatalf("unable to read the rendered definition: %v", err)
	}
	if !strings.Contains(string(b), "version: 1.37.1-k3s1") {
		t.Fatalf("the later values file did not win:\n%s", b)
	}
}
