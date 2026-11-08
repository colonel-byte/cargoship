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

import "testing"

// The k3s image tag spells a build with a hyphen where the release tag uses a plus, because a
// registry reference cannot carry one.
func TestExampleK3sImageRef(t *testing.T) {
	for _, tc := range []struct {
		version string
		want    string
	}{
		{
			version: "1.37.2-k3s1",
			want:    "oci://docker.io/rancher/k3s:v1.37.2-k3s1",
		},
		{
			version: "1.36.4-k3s1",
			want:    "oci://docker.io/rancher/k3s:v1.36.4-k3s1",
		},
	} {
		t.Run(tc.version, func(t *testing.T) {
			if got := exampleK3sImageRef(tc.version); got != tc.want {
				t.Errorf("exampleK3sImageRef(%q) = %q, want %q", tc.version, got, tc.want)
			}
		})
	}
}

// A cached digest rebuilds the same pinned reference the resolve produced, so a second render
// reads no manifests at all.
func TestExampleK3sPinned(t *testing.T) {
	const ref = "oci://docker.io/rancher/k3s:v1.37.2-k3s1"
	const digest = "21ea6e0fac4e3930c2ce8aa40ddd41a8b18cb3e9a0222c030dc005e7e88c4332"
	want := "oci://docker.io/rancher/k3s@sha256:" + digest
	if got := exampleK3sPinned(ref, digest); got != want {
		t.Errorf("exampleK3sPinned = %q, want %q", got, want)
	}
}

// A flavor that takes its binary from an image renders no release URL, and one that does not
// renders no reference - the two paths are exclusive.
func TestNewExampleVersionBinarySource(t *testing.T) {
	var spec exampleDistroSpec
	for _, d := range exampleDistros {
		if d.name == "k3s" {
			spec = d
			break
		}
	}
	if spec.derive == nil {
		t.Fatal("no k3s distro spec with a derive hook")
	}
	for _, tc := range []struct {
		name   string
		flavor exampleFlavor
		oci    bool
	}{
		{
			name:   "release download",
			flavor: exampleFlavor{cni: "flannel", arches: exampleMultiArches},
		},
		{
			name:   "image",
			flavor: exampleFlavor{cni: "flannel", arches: exampleMultiArches, binaryOCI: true},
			oci:    true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := newExampleVersion("v1.37.2+k3s1", spec, tc.flavor)
			if err != nil {
				t.Fatalf("newExampleVersion: %v", err)
			}
			spec.derive(&v)
			if v.BinaryOCI != tc.oci {
				t.Fatalf("BinaryOCI = %v, want %v", v.BinaryOCI, tc.oci)
			}
			if tc.oci {
				if v.BinaryOCIRef == "" {
					t.Error("no image reference derived")
				}
				for _, a := range v.Arches {
					if a.BinaryURL != "" {
						t.Errorf("%s still carries a release URL %q", a.Arch, a.BinaryURL)
					}
				}
				return
			}
			if v.BinaryOCIRef != "" {
				t.Errorf("a release flavor derived the image reference %q", v.BinaryOCIRef)
			}
		})
	}
}
