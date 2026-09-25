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

package layout

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/pkg/helmvalues"
	"github.com/k0sproject/dig"
)

func TestIsContainedPath(t *testing.T) {
	contained := []string{
		"values/0-values.yaml",
		"values.yaml",
		"a/b/../c.yaml",
		"./values.yaml",
	}
	for _, p := range contained {
		if !IsContainedPath(p) {
			t.Errorf("IsContainedPath(%q) = false, want true", p)
		}
	}

	escapes := []string{
		"",
		"..",
		"../values.yaml",
		"values/../../values.yaml",
		"/etc/values.yaml",
		`C:\values.yaml`,
		"https://example.com/values.yaml",
		"oci://example.com/values.yaml",
	}
	for _, p := range escapes {
		if IsContainedPath(p) {
			t.Errorf("IsContainedPath(%q) = true, want false", p)
		}
	}
}

// valuesPathDistro builds the minimum distro validateDistroPaths accepts, so a
// failure can only come from the values paths under test.
func valuesPathDistro(files []string, schema string) distro.ZarfDistro {
	d := distro.ZarfDistro{}
	d.Metadata.Name = "test"
	d.Metadata.Version = "1.0.0"
	d.Spec.Values = distro.ZarfDistroValues{Files: files, Schema: schema}
	return d
}

func TestValidateDistroPathsRejectsEscapingValues(t *testing.T) {
	tests := []struct {
		name   string
		files  []string
		schema string
		want   string
	}{
		{"traversing file", []string{"../../secrets.yaml"}, "", "values file"},
		{"absolute file", []string{"/etc/values.yaml"}, "", "values file"},
		{"remote file", []string{"https://example.com/values.yaml"}, "", "values file"},
		{"traversing schema", nil, "../schema.json", "values schema"},
		{"remote schema", nil, "https://example.com/schema.json", "values schema"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDistroPaths(valuesPathDistro(tt.files, tt.schema))
			if err == nil {
				t.Fatalf("validateDistroPaths accepted %v %q", tt.files, tt.schema)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestValidateDistroPathsAcceptsPackagedValues(t *testing.T) {
	d := valuesPathDistro([]string{"values/0-values.yaml"}, "values/values.schema.json")
	if err := validateDistroPaths(d); err != nil {
		t.Fatalf("validateDistroPaths rejected packaged values: %v", err)
	}
}

func TestLoadValues(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "values"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "values", name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("0-values.yaml", "replicas: 1\n")
	write("1-values.yaml", "replicas: 4\n")
	write("values.schema.json", `{"type":"object","properties":{"replicas":{"type":"integer","maximum":3}}}`)

	vals := distro.ZarfDistroValues{Files: []string{"values/0-values.yaml", "values/1-values.yaml"}}
	merged, err := LoadValues(context.Background(), dir, vals)
	if err != nil {
		t.Fatal(err)
	}
	if merged["replicas"] != 4 {
		t.Fatalf("replicas = %v, want the later file to win with 4", merged["replicas"])
	}

	vals.Schema = "values/values.schema.json"
	if _, err := LoadValues(context.Background(), dir, vals); err == nil {
		t.Fatal("LoadValues accepted values that violate the schema")
	}
}

func TestLoadValuesOverrides(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "values"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "values", name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("0-values.yaml", "cilium:\n  enabled: false\n  ipam:\n    mode: cluster-pool\n")
	write("values.schema.json", `{
		"type": "object",
		"properties": {
			"cilium": {
				"type": "object",
				"properties": {"enabled": {"type": "boolean"}}
			}
		}
	}`)
	vals := distro.ZarfDistroValues{
		Files:  []string{"values/0-values.yaml"},
		Schema: "values/values.schema.json",
	}

	// An override replaces only the keys it names; the rest of the package
	// defaults survive underneath it.
	override := map[string]any{"cilium": map[string]any{"enabled": true}}
	merged, err := LoadValues(context.Background(), dir, vals, override)
	if err != nil {
		t.Fatal(err)
	}
	cilium, ok := merged["cilium"].(map[string]any)
	if !ok {
		t.Fatalf("cilium = %#v, want a map", merged["cilium"])
	}
	if cilium["enabled"] != true {
		t.Fatalf("cilium.enabled = %v, want the override to win with true", cilium["enabled"])
	}
	ipam, ok := cilium["ipam"].(map[string]any)
	if !ok || ipam["mode"] != "cluster-pool" {
		t.Fatalf("cilium.ipam = %#v, want the package default to survive", cilium["ipam"])
	}

	// An override that breaks the schema has to fail like a bad values file.
	bad := map[string]any{"cilium": map[string]any{"enabled": "yes"}}
	if _, err := LoadValues(context.Background(), dir, vals, bad); err == nil {
		t.Fatal("LoadValues accepted an override that violates the schema")
	}
}

// ciliumLayout builds a layout whose engine configuration carries the cilium
// manifest values a mapping moves.
func ciliumLayout(mappings []distro.ZarfDistroValueMapping) *DistroLayout {
	d := distro.ZarfDistro{}
	d.Spec.Values.Mappings = mappings
	d.Spec.Config.Engine = dig.Mapping{
		"manifest": dig.Mapping{
			"rke2-cilium": dig.Mapping{
				"encryption": dig.Mapping{"enabled": false, "type": "wireguard"},
			},
		},
	}
	return &DistroLayout{Distro: d}
}

func cilium(t *testing.T, l *DistroLayout) map[string]any {
	t.Helper()
	got, ok, err := helmvalues.GetValuePath(l.Distro.Spec.Config.Engine, ".manifest.rke2-cilium.encryption")
	if err != nil || !ok {
		t.Fatalf("encryption block missing: ok=%v err=%v", ok, err)
	}
	// An untouched branch is still the dig.Mapping the package was decoded into;
	// one a mapping wrote through comes back as map[string]any over the same
	// data. Both are maps for the purpose of reading a key.
	switch m := got.(type) {
	case map[string]any:
		return m
	case dig.Mapping:
		return m
	}
	t.Fatalf("encryption = %#v, want a map", got)
	return nil
}

func TestApplyValues(t *testing.T) {
	mappings := []distro.ZarfDistroValueMapping{
		{Source: ".cilium.encryption.enabled", Target: ".manifest.rke2-cilium.encryption.enabled"},
	}

	t.Run("a value the cluster sets is projected", func(t *testing.T) {
		l := ciliumLayout(mappings)
		values := map[string]any{"cilium": map[string]any{"encryption": map[string]any{"enabled": true}}}
		if err := l.ApplyValues(values); err != nil {
			t.Fatal(err)
		}
		enc := cilium(t, l)
		if enc["enabled"] != true {
			t.Fatalf("encryption.enabled = %v, want true", enc["enabled"])
		}
		// The mapping names one key, so the rest of the block is left as the
		// package shipped it.
		if enc["type"] != "wireguard" {
			t.Fatalf("encryption.type = %v, want wireguard", enc["type"])
		}
	})

	t.Run("a value no one sets keeps the package default", func(t *testing.T) {
		l := ciliumLayout(mappings)
		if err := l.ApplyValues(map[string]any{}); err != nil {
			t.Fatal(err)
		}
		if enc := cilium(t, l); enc["enabled"] != false {
			t.Fatalf("encryption.enabled = %v, want the package default false", enc["enabled"])
		}
	})

	t.Run("no mappings is a no-op", func(t *testing.T) {
		l := ciliumLayout(nil)
		values := map[string]any{"cilium": map[string]any{"encryption": map[string]any{"enabled": true}}}
		if err := l.ApplyValues(values); err != nil {
			t.Fatal(err)
		}
		if enc := cilium(t, l); enc["enabled"] != false {
			t.Fatalf("encryption.enabled = %v, want it untouched", enc["enabled"])
		}
	})

	t.Run("a bad target path is an error", func(t *testing.T) {
		l := ciliumLayout([]distro.ZarfDistroValueMapping{
			{Source: ".cilium.encryption.enabled", Target: "manifest.rke2-cilium"},
		})
		values := map[string]any{"cilium": map[string]any{"encryption": map[string]any{"enabled": true}}}
		if err := l.ApplyValues(values); err == nil {
			t.Fatal("ApplyValues accepted a target path without a leading dot")
		}
	})
}

// A mapping copies whatever the value is, so a list is how a package exposes a
// key the engine reads as one -- the disable list every rke2 and k3s example
// projects .addons.disabled onto. Nothing else can produce one: a template
// renders into a scalar, so a list has to arrive through a mapping.
func TestApplyValuesProjectsAList(t *testing.T) {
	layout := func() *DistroLayout {
		d := distro.ZarfDistro{}
		d.Spec.Values.Mappings = []distro.ZarfDistroValueMapping{
			{Source: ".addons.disabled", Target: ".config.disable"},
		}
		d.Spec.Config.Engine = dig.Mapping{
			"config": dig.Mapping{"disable": []any{"rke2-ingress-nginx"}},
		}
		return &DistroLayout{Distro: d}
	}

	t.Run("the cluster's list replaces the package's", func(t *testing.T) {
		l := layout()
		values := map[string]any{"addons": map[string]any{
			"disabled": []any{"rke2-ingress-nginx", "rke2-traefik", "rke2-traefik-crd"},
		}}
		if err := l.ApplyValues(values); err != nil {
			t.Fatal(err)
		}
		// Read it the way the distro does, through the mapping Dup rebuilds: a
		// list that came back as anything else would be written to config.yaml
		// as that instead.
		got := l.Distro.Spec.Config.Engine.DigMapping("config")["disable"]
		want := []any{"rke2-ingress-nginx", "rke2-traefik", "rke2-traefik-crd"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("config.disable = %#v, want %#v", got, want)
		}
	})

	t.Run("an empty list is still a list", func(t *testing.T) {
		l := layout()
		if err := l.ApplyValues(map[string]any{"addons": map[string]any{"disabled": []any{}}}); err != nil {
			t.Fatal(err)
		}
		if got := l.Distro.Spec.Config.Engine.DigMapping("config")["disable"]; !reflect.DeepEqual(got, []any{}) {
			t.Fatalf("config.disable = %#v, want an empty list", got)
		}
	})

	t.Run("a cluster that sets nothing keeps the package's list", func(t *testing.T) {
		l := layout()
		if err := l.ApplyValues(map[string]any{}); err != nil {
			t.Fatal(err)
		}
		got := l.Distro.Spec.Config.Engine.DigMapping("config")["disable"]
		if !reflect.DeepEqual(got, []any{"rke2-ingress-nginx"}) {
			t.Fatalf("config.disable = %#v, want the package default", got)
		}
	})
}

// vsphereCPI is a chart entry written the other way the manifest section allows:
// a YAML string rather than a mapping. The vsphere examples mix the two, since
// the cloud provider values are carried verbatim while cilium's are structured.
const vsphereCPI = `vCenter:
  host: ""
  port: 443
`

// mixedLayout builds a layout whose manifest section holds both kinds of chart
// entry, so a mapping that writes into the structured one has string siblings.
func mixedLayout(mappings []distro.ZarfDistroValueMapping) *DistroLayout {
	d := distro.ZarfDistro{}
	d.Spec.Values.Mappings = mappings
	d.Spec.Config.Engine = dig.Mapping{
		"manifest": dig.Mapping{
			"rke2-cilium": dig.Mapping{
				"encryption": dig.Mapping{"enabled": false, "type": "wireguard"},
			},
			"rancher-vsphere-cpi": vsphereCPI,
		},
	}
	return &DistroLayout{Distro: d}
}

func TestApplyValuesMixedManifest(t *testing.T) {
	mappings := []distro.ZarfDistroValueMapping{
		{Source: ".cilium.encryption.enabled", Target: ".manifest.rke2-cilium.encryption.enabled"},
	}
	values := map[string]any{"cilium": map[string]any{"encryption": map[string]any{"enabled": true}}}

	t.Run("a string sibling is left alone", func(t *testing.T) {
		l := mixedLayout(mappings)
		if err := l.ApplyValues(values); err != nil {
			t.Fatal(err)
		}
		if enc := cilium(t, l); enc["enabled"] != true {
			t.Fatalf("encryption.enabled = %v, want true", enc["enabled"])
		}
		got, ok, err := helmvalues.GetValuePath(l.Distro.Spec.Config.Engine, `.manifest.rancher-vsphere-cpi`)
		if err != nil || !ok {
			t.Fatalf("vsphere entry missing: ok=%v err=%v", ok, err)
		}
		if got != vsphereCPI {
			t.Fatalf("vsphere entry = %#v, want the string the package shipped", got)
		}
	})

	// dig replaces a value that is not a Mapping with an empty one, so a branch
	// left as a plain map by a write reads back empty. Every chart has to still
	// be there for the caller that renders the HelmChartConfig files.
	t.Run("the manifest section still digs", func(t *testing.T) {
		l := mixedLayout(mappings)
		if err := l.ApplyValues(values); err != nil {
			t.Fatal(err)
		}
		engine := l.Distro.Spec.Config.Engine
		manifest := engine.DigMapping("manifest")
		if len(manifest) != 2 {
			t.Fatalf("manifest holds %d charts, want 2: %#v", len(manifest), manifest)
		}
		if _, ok := manifest["rancher-vsphere-cpi"].(string); !ok {
			t.Fatalf("vsphere entry = %#v, want a string", manifest["rancher-vsphere-cpi"])
		}
		enabled := engine.Dig("manifest", "rke2-cilium", "encryption", "enabled")
		if enabled != true {
			t.Fatalf("encryption.enabled = %v, want true", enabled)
		}
	})

	t.Run("a target inside a string entry is an error", func(t *testing.T) {
		l := mixedLayout([]distro.ZarfDistroValueMapping{
			{Source: ".cilium.encryption.enabled", Target: `.manifest.rancher-vsphere-cpi.vCenter.host`},
		})
		if err := l.ApplyValues(values); err == nil {
			t.Fatal("ApplyValues wrote through a chart entry that is a string")
		}
	})
}

// templatedLayout builds a layout whose engine configuration is written with
// templates rather than mappings: one scalar that stands for a whole value, and
// one chart entry written as a string so a conditional can decide what it holds.
func templatedLayout() *DistroLayout {
	d := distro.ZarfDistro{}
	d.Spec.Config.Engine = dig.Mapping{
		"manifest": dig.Mapping{
			"rke2-cilium": dig.Mapping{
				"encryption": dig.Mapping{
					"enabled": "{{ .Values.cilium.encryption.enabled }}",
					"type":    "wireguard",
				},
			},
			"rke2-coredns": "replicas: {{ .Values.coredns.replicas }}\n" +
				"{{- if .Values.coredns.autoscale }}\nautoscaler:\n  enabled: true\n{{- end }}",
		},
	}
	return &DistroLayout{Distro: d}
}

func templateValues(encryption bool, autoscale bool) map[string]any {
	return map[string]any{
		"cilium":  map[string]any{"encryption": map[string]any{"enabled": encryption}},
		"coredns": map[string]any{"replicas": 2, "autoscale": autoscale},
	}
}

func TestApplyValuesRendersTemplates(t *testing.T) {
	// A scalar that is nothing but one template action comes back as the type the
	// value has, not as the text of it, so the engine reads a YAML boolean.
	t.Run("a templated scalar keeps its type", func(t *testing.T) {
		l := templatedLayout()
		if err := l.ApplyValues(templateValues(true, false)); err != nil {
			t.Fatal(err)
		}
		if enc := cilium(t, l); enc["enabled"] != true {
			t.Fatalf("encryption.enabled = %#v, want the boolean true", enc["enabled"])
		}
	})

	// The thing a mapping cannot do: decide whether a block is written at all.
	t.Run("a conditional decides what a chart entry holds", func(t *testing.T) {
		for _, tt := range []struct {
			autoscale bool
			want      string
		}{
			{true, "autoscaler"},
			{false, "replicas: 2"},
		} {
			l := templatedLayout()
			if err := l.ApplyValues(templateValues(false, tt.autoscale)); err != nil {
				t.Fatal(err)
			}
			got, ok := l.Distro.Spec.Config.Engine.Dig("manifest", "rke2-coredns").(string)
			if !ok {
				t.Fatalf("coredns entry is %#v, want a string", l.Distro.Spec.Config.Engine.Dig("manifest", "rke2-coredns"))
			}
			if !strings.Contains(got, tt.want) {
				t.Fatalf("coredns entry = %q, want it to contain %q", got, tt.want)
			}
			if strings.Contains(got, "autoscaler") != tt.autoscale {
				t.Fatalf("coredns entry = %q, want the autoscaler block only when the value is set", got)
			}
		}
	})

	// Rendering runs before the mappings, so only what the package author wrote is
	// executed. A cluster that sets a value holding {{ gets it written through as
	// the text it is.
	t.Run("a value that looks like a template is not executed", func(t *testing.T) {
		l := templatedLayout()
		l.Distro.Spec.Values.Mappings = []distro.ZarfDistroValueMapping{
			{Source: ".cilium.encryption.type", Target: ".manifest.rke2-cilium.encryption.type"},
		}
		values := templateValues(false, false)
		ciliumValues, ok := values["cilium"].(map[string]any)
		if !ok {
			t.Fatalf("cilium = %#v, want a map", values["cilium"])
		}
		encryption, ok := ciliumValues["encryption"].(map[string]any)
		if !ok {
			t.Fatalf("cilium.encryption = %#v, want a map", ciliumValues["encryption"])
		}
		encryption["type"] = "{{ .Values.cilium.encryption.enabled }}"

		if err := l.ApplyValues(values); err != nil {
			t.Fatal(err)
		}
		if got := cilium(t, l)["type"]; got != "{{ .Values.cilium.encryption.enabled }}" {
			t.Fatalf("encryption.type = %#v, want the literal text the cluster set", got)
		}
	})

	t.Run("a template naming a value no one defines is an error", func(t *testing.T) {
		l := templatedLayout()
		if err := l.ApplyValues(map[string]any{}); err == nil {
			t.Fatal("ApplyValues rendered a template whose values are missing")
		}
	})
}

// stageFile writes a file where the packager stages it, under the index of its
// entry in the spec, which is the coupling the upload phases read it back by.
func stageFile(t *testing.T, root, dir string, idx int, name, content string) string {
	t.Helper()
	path := filepath.Join(root, dir, strconv.Itoa(idx), name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRenderFiles(t *testing.T) {
	root := t.TempDir()
	tmpl := "address: {{ .Values.registry.address }}\n"

	rendered := stageFile(t, root, string(config.FilesDir), 0, "registries.yaml", tmpl)
	verbatim := stageFile(t, root, string(config.FilesDir), 1, "untouched.yaml", tmpl)
	osFile := stageFile(t, root, string(config.OSDir), 0, "motd", "welcome to {{ .Values.registry.address }}\n")

	yes := true
	d := distro.ZarfDistro{}
	d.Spec.Config.Files = v1alpha1.ZarfFiles{
		{Target: "/etc/rancher/rke2/registries.yaml", Template: &yes},
		{Target: "/etc/untouched.yaml"},
	}
	d.Spec.Config.OS.Files = v1alpha1.ZarfFiles{{Target: "/etc/motd", Template: &yes}}
	l := &DistroLayout{Distro: d, dirPath: root}

	values := map[string]any{"registry": map[string]any{"address": "127.0.0.1:31999"}}
	if err := l.RenderFiles(context.Background(), values); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		path string
		want string
	}{
		{rendered, "address: 127.0.0.1:31999\n"},
		{verbatim, tmpl},
		{osFile, "welcome to 127.0.0.1:31999\n"},
	} {
		got, err := os.ReadFile(tt.path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != tt.want {
			t.Fatalf("%s = %q, want %q", tt.path, got, tt.want)
		}
	}

	// A staged file keeps the mode it was staged with: some of these carry
	// registry credentials, and a rewrite is not a reason to widen them.
	info, err := os.Stat(rendered)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v, want the mode the file was staged with", info.Mode().Perm())
	}
}

func TestRenderFilesReportsAMissingValue(t *testing.T) {
	root := t.TempDir()
	stageFile(t, root, string(config.FilesDir), 0, "registries.yaml", "address: {{ .Values.registry.address }}\n")

	yes := true
	d := distro.ZarfDistro{}
	d.Spec.Config.Files = v1alpha1.ZarfFiles{{Target: "/etc/rancher/rke2/registries.yaml", Template: &yes}}
	l := &DistroLayout{Distro: d, dirPath: root}

	if err := l.RenderFiles(context.Background(), map[string]any{}); err == nil {
		t.Fatal("RenderFiles rendered a file whose values are missing")
	}
}
