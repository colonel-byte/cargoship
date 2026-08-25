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

	got, err := loadRegistryOverrides(cfgPath)
	if err != nil {
		t.Fatalf("loadRegistryOverrides failed: %v", err)
	}

	want := map[string]string{
		"docker.io":         "mirror.example.com",
		"docker.io/library": "library-mirror.example.com",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("loadRegistryOverrides() = %+v, want %+v", got, want)
	}
}

func TestLoadRegistryOverridesEmpty(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "cargoship-config.yaml")
	writeFile(t, cfgPath, "log_level: info\n")

	got, err := loadRegistryOverrides(cfgPath)
	if err != nil {
		t.Fatalf("loadRegistryOverrides failed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("loadRegistryOverrides() = %+v, want empty", got)
	}
}

func TestLoadRegistryOverridesMissingFile(t *testing.T) {
	if _, err := loadRegistryOverrides(filepath.Join(t.TempDir(), "does-not-exist.yaml")); err == nil {
		t.Fatalf("loadRegistryOverrides() = nil error, want error")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writeFile failed: %v", err)
	}
}
