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

package assemble

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/pkg/packager/layout"
)

func writeValuesFile(t *testing.T, dir string, name string, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// valuesDistro builds a distro whose only interesting content is its values.
func valuesDistro(files []string, schema string) distro.ZarfDistro {
	d := distro.ZarfDistro{}
	d.Spec.Values = distro.ZarfDistroValues{Files: files, Schema: schema}
	return d
}

func TestPackageValuesCopiesAndRewritesPaths(t *testing.T) {
	src := t.TempDir()
	build := t.TempDir()
	writeValuesFile(t, src, "values.yaml", "replicas: 1\nname: base\n")
	writeValuesFile(t, src, "overrides/values.yaml", "replicas: 3\n")

	d, err := packageValues(context.Background(),
		valuesDistro([]string{"values.yaml", "overrides/values.yaml"}, ""), src, build)
	if err != nil {
		t.Fatal(err)
	}

	// Both files share a base name, so the index prefix is what keeps the
	// second from overwriting the first.
	want := []string{"values/0-values.yaml", "values/1-values.yaml"}
	if !reflect.DeepEqual(d.Spec.Values.Files, want) {
		t.Fatalf("Spec.Values.Files = %v, want %v", d.Spec.Values.Files, want)
	}
	for _, rel := range want {
		if _, err := os.Stat(filepath.Join(build, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("values file not in package: %v", err)
		}
	}

	merged, err := layout.LoadValues(context.Background(), build, d.Spec.Values)
	if err != nil {
		t.Fatal(err)
	}
	if merged["replicas"] != 3 {
		t.Fatalf("replicas = %v, want the later file to win with 3", merged["replicas"])
	}
	if merged["name"] != "base" {
		t.Fatalf("name = %v, want base", merged["name"])
	}
}

func TestPackageValuesNoValuesIsNoOp(t *testing.T) {
	src := t.TempDir()
	build := t.TempDir()

	d, err := packageValues(context.Background(), valuesDistro(nil, ""), src, build)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Spec.Values.Files) != 0 || d.Spec.Values.Schema != "" {
		t.Fatalf("Spec.Values = %+v, want it untouched", d.Spec.Values)
	}
	// An empty values directory would still be checksummed and shipped.
	if _, err := os.Stat(filepath.Join(build, config.ValuesDir)); !os.IsNotExist(err) {
		t.Fatalf("values directory created for a package with no values: %v", err)
	}
}

func TestPackageValuesSchemaIsPackagedAndEnforced(t *testing.T) {
	src := t.TempDir()
	build := t.TempDir()
	writeValuesFile(t, src, "values.yaml", "replicas: 2\n")
	writeValuesFile(t, src, "values.schema.json", `{
		"type": "object",
		"properties": {"replicas": {"type": "integer", "minimum": 1}},
		"required": ["replicas"]
	}`)

	d, err := packageValues(context.Background(),
		valuesDistro([]string{"values.yaml"}, "values.schema.json"), src, build)
	if err != nil {
		t.Fatal(err)
	}
	if d.Spec.Values.Schema != "values/values.schema.json" {
		t.Fatalf("Spec.Values.Schema = %q, want values/values.schema.json", d.Spec.Values.Schema)
	}
	if _, err := os.Stat(filepath.Join(build, config.ValuesDir, config.ValuesSchema)); err != nil {
		t.Fatalf("schema not in package: %v", err)
	}
}

func TestPackageValuesSchemaMismatchFailsTheBuild(t *testing.T) {
	src := t.TempDir()
	build := t.TempDir()
	writeValuesFile(t, src, "values.yaml", "replicas: 0\n")
	writeValuesFile(t, src, "values.schema.json", `{
		"type": "object",
		"properties": {"replicas": {"type": "integer", "minimum": 1}}
	}`)

	_, err := packageValues(context.Background(),
		valuesDistro([]string{"values.yaml"}, "values.schema.json"), src, build)
	if err == nil {
		t.Fatal("packageValues succeeded with values that violate the schema")
	}
	if !strings.Contains(err.Error(), "do not satisfy the schema") {
		t.Fatalf("error = %v, want it to name the schema mismatch", err)
	}
}

func TestPackageValuesMissingFile(t *testing.T) {
	src := t.TempDir()
	build := t.TempDir()

	_, err := packageValues(context.Background(), valuesDistro([]string{"absent.yaml"}, ""), src, build)
	if err == nil {
		t.Fatal("packageValues succeeded with a values file that does not exist")
	}
}
