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

package distrogen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/colonel-byte/cargoship/pkg/helmvalues"
)

// writeTemplate writes a template into a temporary directory and returns its path.
func writeTemplate(t *testing.T, name, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("unable to write the template: %v", err)
	}
	return path
}

func TestOutputName(t *testing.T) {
	cases := []struct {
		name string
		file File
		want string
	}{
		{
			name: "the template's own name",
			file: File{
				Template: "/tmp/values.yaml.tmpl",
			},
			want: "values.yaml",
		},
		{
			name: "a declared name wins",
			file: File{
				Template: "/tmp/k3s-distro.yaml.tmpl",
				Name:     "distro.yaml",
			},
			want: "distro.yaml",
		},
		{
			name: "a template with no suffix",
			file: File{
				Template: "/tmp/values.yaml",
			},
			want: "values.yaml",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.file.OutputName(); got != tc.want {
				t.Fatalf("OutputName() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRenderUsesValuesAndSprigFuncs(t *testing.T) {
	path := writeTemplate(t, "distro.yaml.tmpl", "version: {{ .version }}\nname: {{ .name | upper }}\n")

	rendered, err := Render([]File{{Template: path}}, map[string]any{
		"version": "1.36.4",
		"name":    "k3s",
	})
	if err != nil {
		t.Fatalf("Render() returned %v", err)
	}
	if len(rendered) != 1 {
		t.Fatalf("Render() returned %d files, want 1", len(rendered))
	}
	if got, want := rendered[0].Name, "distro.yaml"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
	if got, want := string(rendered[0].Content), "version: 1.36.4\nname: K3S\n"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

// A missing key is the mistake this generator exists to catch: a definition carrying the literal
// <no value> renders, writes, and fails much later as a package nothing can install.
func TestRenderRejectsAMissingKey(t *testing.T) {
	path := writeTemplate(t, "distro.yaml.tmpl", "version: {{ .nosuchkey }}\n")

	if _, err := Render([]File{{Template: path}}, map[string]any{}); err == nil {
		t.Fatal("Render() accepted a template naming a key the values do not set")
	}
}

func TestRenderWithInjectedFuncs(t *testing.T) {
	path := writeTemplate(t, "a.yaml.tmpl", "{{ shout .name }}\n")

	funcs := helmvalues.WithFuncs(template.FuncMap{
		"shout": func(s string) string { return strings.ToUpper(s) + "!" },
	})

	rendered, err := Render([]File{{Template: path}}, map[string]any{"name": "shared"}, funcs)
	if err != nil {
		t.Fatalf("Render() returned %v", err)
	}
	if got, want := string(rendered[0].Content), "SHARED!\n"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

// A template that renders to its own path is overwritten by its own output, and the read
// happens first, so without this check it succeeds and the template is gone.
func TestCheckOutputsRefusesToWriteOverATemplate(t *testing.T) {
	path := writeTemplate(t, "distro.yaml", "kind: Distro\n")
	dir := filepath.Dir(path)

	cases := []struct {
		name  string
		dir   string
		files []File
		want  bool
	}{
		{
			name: "an output landing on its own template",
			dir:  dir,
			files: []File{
				{Template: path},
			},
			want: false,
		},
		{
			name: "the same template rendered elsewhere",
			dir:  t.TempDir(),
			files: []File{
				{Template: path},
			},
			want: true,
		},
		{
			name: "two templates colliding on one name",
			dir:  t.TempDir(),
			files: []File{
				{Template: filepath.Join("a", "values.yaml.tmpl")},
				{Template: filepath.Join("b", "values.yaml.tmpl")},
			},
			want: false,
		},
		{
			name: "a collision the declared names avoid",
			dir:  t.TempDir(),
			files: []File{
				{Template: filepath.Join("a", "values.yaml.tmpl"), Name: "a-values.yaml"},
				{Template: filepath.Join("b", "values.yaml.tmpl")},
			},
			want: true,
		},
		{
			name: "a template whose whole name is the suffix",
			dir:  t.TempDir(),
			files: []File{
				{Template: ".tmpl"},
			},
			want: false,
		},
		{
			name: "a name that would escape the output directory",
			dir:  t.TempDir(),
			files: []File{
				{Template: "distro.yaml.tmpl", Name: filepath.Join("..", "distro.yaml")},
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckOutputs(tc.dir, tc.files)
			if tc.want && err != nil {
				t.Fatalf("CheckOutputs() refused a safe render: %v", err)
			}
			if !tc.want && err == nil {
				t.Fatal("CheckOutputs() accepted a render that would lose a file")
			}
		})
	}
}

func TestWriteCreatesTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "v1.36", "v1.36.4")

	paths, err := Write(dir, []Rendered{{
		Name:    "distro.yaml",
		Content: []byte("kind: Distro\n"),
	}})
	if err != nil {
		t.Fatalf("Write() returned %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("Write() wrote %d files, want 1", len(paths))
	}
	b, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatalf("unable to read what was written: %v", err)
	}
	if got, want := string(b), "kind: Distro\n"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestValidateDistro(t *testing.T) {
	valid := `apiVersion: colonel-byte.dev/v1alpha1
kind: Distro
metadata:
  name: example
  version: 1.36.4-k3s1
spec:
  type: k3s
  version: 1.36.4-k3s1
  config:
    os: {}
`

	if err := ValidateDistro("distro.yaml", []byte(valid)); err != nil {
		t.Fatalf("ValidateDistro() rejected a valid definition: %v", err)
	}

	// metadata.name is required, so a definition without it is one `cargoship create` would
	// refuse -- which is the whole reason the check happens before anything is written.
	if err := ValidateDistro("distro.yaml", []byte("apiVersion: colonel-byte.dev/v1alpha1\nkind: Distro\nspec:\n  type: k3s\n")); err == nil {
		t.Fatal("ValidateDistro() accepted a definition with no metadata")
	}
}
