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

	"github.com/colonel-byte/cargoship/types"
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

func TestExpandHomePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := types.DistroConfig{
		CachePath:     "~/.cargoship-cache",
		TempDirectory: "~/staging",
		AgeOpts: types.AgeOptions{
			IdentityFiles:   []string{"~/.age/cargoship.key", "/etc/cargoship/ci.key"},
			RecipientsFiles: []string{"~/.ssh/authorized_keys"},
			Recipients:      []string{"age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p"},
		},
		DistroOpts: types.DistroOptions{
			KubeConfig:  "~/.kube/config",
			Output:      "~/packages",
			PublicKey:   "~/.cosign/cosign.pub",
			TrustedRoot: "~/.sigstore/trusted_root.json",
			PublishOpts: types.DistroPublishOptions{
				// A cosign key provider rather than a file: it has no leading ~, so it
				// must survive untouched.
				SigningKey: "awskms:///alias/cargoship",
			},
		},
	}

	if err := expandHomePaths(&cfg); err != nil {
		t.Fatalf("expandHomePaths() error = %v", err)
	}

	want := types.DistroConfig{
		CachePath:     filepath.Join(home, ".cargoship-cache"),
		TempDirectory: filepath.Join(home, "staging"),
		AgeOpts: types.AgeOptions{
			IdentityFiles:   []string{filepath.Join(home, ".age", "cargoship.key"), "/etc/cargoship/ci.key"},
			RecipientsFiles: []string{filepath.Join(home, ".ssh", "authorized_keys")},
			Recipients:      []string{"age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p"},
		},
		DistroOpts: types.DistroOptions{
			KubeConfig:  filepath.Join(home, ".kube", "config"),
			Output:      filepath.Join(home, "packages"),
			PublicKey:   filepath.Join(home, ".cosign", "cosign.pub"),
			TrustedRoot: filepath.Join(home, ".sigstore", "trusted_root.json"),
			PublishOpts: types.DistroPublishOptions{
				SigningKey: "awskms:///alias/cargoship",
			},
		},
	}

	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("expandHomePaths() = %+v, want %+v", cfg, want)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writeFile failed: %v", err)
	}
}
