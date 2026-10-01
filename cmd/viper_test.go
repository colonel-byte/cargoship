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
	"reflect"
	"testing"
)

func TestLoadRegistryOverridesDottedKeys(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "cargoship-config.yaml")
	writeFile(t, cfgPath, `
distro:
  create:
    registry_override:
      docker.io: mirror.example.com
      docker.io/library: library-mirror.example.com
`)

	createOpts, err := loadCreateOverrides(cfgPath)
	if err != nil {
		t.Fatalf("loadCreateOverrides failed: %v", err)
	}
	got := map[string]string(createOpts.RegistryOverride)

	want := map[string]string{
		"docker.io":         "mirror.example.com",
		"docker.io/library": "library-mirror.example.com",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RegistryOverride = %+v, want %+v", got, want)
	}
}

// TestLoadCreateOverridesURLKeys is the test that proves the raw-YAML bypass earns its keep for
// file_override: a URL key carries ".", ":" and "/", every one of which viper would mangle.
func TestLoadCreateOverridesURLKeys(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "cargoship-config.yaml")
	writeFile(t, cfgPath, `
distro:
  create:
    file_override:
      https://rpm.rancher.io: https://mirror.example.com/rpm-rancher
      https://github.com/rancher: /srv/staged/rancher
`)

	createOpts, err := loadCreateOverrides(cfgPath)
	if err != nil {
		t.Fatalf("loadCreateOverrides failed: %v", err)
	}

	want := map[string]string{
		"https://rpm.rancher.io":     "https://mirror.example.com/rpm-rancher",
		"https://github.com/rancher": "/srv/staged/rancher",
	}
	if !reflect.DeepEqual(map[string]string(createOpts.FileOverride), want) {
		t.Fatalf("FileOverride = %+v, want %+v", createOpts.FileOverride, want)
	}
}

func TestLoadCreateOverridesEmpty(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "cargoship-config.yaml")
	writeFile(t, cfgPath, "log_level: info\n")

	createOpts, err := loadCreateOverrides(cfgPath)
	if err != nil {
		t.Fatalf("loadCreateOverrides failed: %v", err)
	}
	if len(createOpts.RegistryOverride) != 0 || len(createOpts.FileOverride) != 0 {
		t.Fatalf("loadCreateOverrides() = %+v, want empty", createOpts)
	}
}

func TestLoadCreateOverridesMissingFile(t *testing.T) {
	if _, err := loadCreateOverrides(filepath.Join(t.TempDir(), "does-not-exist.yaml")); err == nil {
		t.Fatalf("loadCreateOverrides() = nil error, want error")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writeFile failed: %v", err)
	}
}
