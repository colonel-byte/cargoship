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

package examples

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestHaulerName(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "rpm with a tilde is renamed, since ~ is not legal in an OCI tag",
			url:  "https://rpm.rancher.io/rke2/latest/1.36/centos/9/x86_64/rke2-common-1.36.5~rke2r1-0.el9.x86_64.rpm",
			want: "rke2-common-1.36.5-rke2r1-0.el9.x86_64.rpm",
		},
		{
			name: "a usable name is left to hauler",
			url:  "https://github.com/rancher/rke2/releases/download/v1.36.5%2Brke2r1/rke2.linux-amd64.tar.gz",
			want: "",
		},
		{
			name: "percent encoding in the path is not a reason to rename",
			url:  "https://example.com/v1.36.5%2Brke2r1/thing.tar.gz",
			want: "",
		},
		{
			name: "no path separator at all",
			url:  "rke2-common-1.36.5~rke2r1.rpm",
			want: "rke2-common-1.36.5-rke2r1.rpm",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := haulerName(tt.url); got != tt.want {
				t.Errorf("haulerName(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

// The architecture-independent policy RPM has to be in every architecture's manifest, or
// collecting one architecture is not enough to build it.
func TestHaulerFilesIncludesArchIndependentAndDeduplicates(t *testing.T) {
	v := exampleVersion{
		SelinuxRPM: "https://rpm.rancher.io/rke2/latest/common/centos/9/noarch/rke2-selinux-0.22-1.el9.noarch.rpm",
		Arches: []exampleArch{{
			Arch:       "amd64",
			CommonRPM:  "https://example.com/common.rpm",
			ServerRPM:  "https://example.com/server.rpm",
			AgentRPM:   "https://example.com/agent.rpm",
			TarballURL: "https://example.com/rke2.linux-amd64.tar.gz",
			EtcdURL:    "https://example.com/etcd.tar.gz",
		}},
	}

	got := v.HaulerFiles("amd64")
	want := []string{
		v.SelinuxRPM,
		"https://example.com/common.rpm",
		"https://example.com/server.rpm",
		"https://example.com/agent.rpm",
		"https://example.com/rke2.linux-amd64.tar.gz",
		"https://example.com/etcd.tar.gz",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("HaulerFiles(amd64) = %v, want %v", got, want)
	}
}

// A distro that installs from one binary leaves every rke2 field empty, and a URL named twice
// is one artifact in the store.
func TestHaulerFilesSkipsEmptyAndRepeatedURLs(t *testing.T) {
	v := exampleVersion{Arches: []exampleArch{{
		Arch:       "arm64",
		BinaryURL:  "https://example.com/k3s-arm64",
		TarballURL: "https://example.com/k3s-arm64",
	}}}

	got := v.HaulerFiles("arm64")
	want := []string{"https://example.com/k3s-arm64"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("HaulerFiles(arm64) = %v, want %v", got, want)
	}
}

// An architecture the version does not target has no files and no arch, which is what lets a
// per-architecture template be skipped rather than rendered empty.
func TestHaulerFilesAndArchForAnUntargetedArchitecture(t *testing.T) {
	v := exampleVersion{Arches: []exampleArch{{Arch: "amd64", CommonRPM: "https://example.com/common.rpm"}}}

	if got := v.HaulerFiles("arm64"); got != nil {
		t.Errorf("HaulerFiles(arm64) = %v, want nil", got)
	}
	if got := v.ArchByName("arm64"); got != nil {
		t.Errorf("ArchByName(arm64) = %v, want nil", got)
	}
	if got := v.ArchByName("amd64"); got == nil || got.Arch != "amd64" {
		t.Errorf("ArchByName(amd64) = %v, want the amd64 entry", got)
	}
}

// A template that guards its body renders only comments for a flavor that does not have what
// it describes, and a file of comments is not a manifest worth writing.
func TestDeclaresManifest(t *testing.T) {
	tests := []struct {
		name     string
		rendered string
		want     bool
	}{
		{
			name:     "a guarded template that rendered nothing",
			rendered: "# Copyright\n#\n# The images .spec.config.engine.manifest.rke2-cilium declares.\n",
			want:     false,
		},
		{
			name:     "a manifest after a comment header",
			rendered: "# Copyright\n#\napiVersion: content.hauler.cattle.io/v1\nkind: Images\n",
			want:     true,
		},
		{
			name:     "a manifest with no header at all",
			rendered: "apiVersion: content.hauler.cattle.io/v1\nkind: Files\n",
			want:     true,
		},
		{
			name:     "apiVersion named inside a comment is not a manifest",
			rendered: "# every manifest carries an apiVersion: line\n",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := declaresManifest([]byte(tt.rendered)); got != tt.want {
				t.Errorf("declaresManifest(%q) = %v, want %v", tt.rendered, got, tt.want)
			}
		})
	}
}

// A flavor whose definition declares no rke2-cilium chart, and only one architecture, must get
// neither a cilium manifest nor an arm64 one -- the templates guard their own bodies.
func TestHaulerManifestsSkipWhatAFlavorDoesNotHave(t *testing.T) {
	// The template paths are relative to the repository root, which is where mage runs.
	t.Chdir(filepath.Join("..", "..", "..", ".."))

	dir := t.TempDir()
	spec := exampleDistroSpec{name: "rke2", haulerTemplates: exampleRKE2Hauler}
	f := exampleFlavor{dir: "example/test", haulerManifests: true}
	v := exampleVersion{
		Name:       "test-flavor",
		CoreImages: []string{"docker.io/rancher/mirrored-pause:3.10.2"},
		SelinuxRPM: "https://rpm.rancher.io/x/rke2-selinux.noarch.rpm",
		Arches:     []exampleArch{{Arch: "amd64", CommonRPM: "https://example.com/common.rpm"}},
	}

	if err := writeExampleHaulerManifests(dir, spec, f, v); err != nil {
		t.Fatalf("writeExampleHaulerManifests: %v", err)
	}

	for _, name := range []string{"hauler-images-core.yaml", "hauler-files-amd64.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s was not written: %v", name, err)
		}
	}
	for _, name := range []string{
		"hauler-files-arm64.yaml",
		"hauler-images-rke2-cilium.yaml",
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s was written for a flavor that has nothing to put in it", name)
		}
	}
}
