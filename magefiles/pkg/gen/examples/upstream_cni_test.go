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
	"encoding/base64"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// testCNIManifest covers what the three CNIs actually publish between them: a plain reference,
// a quoted one with a digest the way helm renders it, the same image twice (an init container
// and a container of one DaemonSet), an unrendered chart placeholder, and the imagePullPolicy
// line that sits next to every one of them.
const testCNIManifest = `apiVersion: apps/v1
kind: DaemonSet
spec:
  template:
    spec:
      initContainers:
        - name: install-cni
          image: ghcr.io/flannel-io/flannel:v0.28.10
          imagePullPolicy: IfNotPresent
      containers:
        - name: kube-flannel
          image: ghcr.io/flannel-io/flannel:v0.28.10
        - name: cilium-agent
          image: "quay.io/cilium/cilium:v1.20.2@sha256:2939231d0d3e3ebddcd80fffa168b7ddcc78fdf0dc864d1c8c126ff523c54f01"
        - name: unset
          image: {{ .Values.image.repository }}
`

func TestManifestImages(t *testing.T) {
	got := manifestImages(splitReleaseLines([]byte(testCNIManifest)))
	want := []string{
		"ghcr.io/flannel-io/flannel:v0.28.10",
		"quay.io/cilium/cilium:v1.20.2@sha256:2939231d0d3e3ebddcd80fffa168b7ddcc78fdf0dc864d1c8c126ff523c54f01",
	}
	if !slices.Equal(got, want) {
		t.Errorf("manifestImages() = %q, want %q", got, want)
	}
}

func TestManifestImagesNone(t *testing.T) {
	if got := manifestImages([]string{"kind: ConfigMap", "imagePullPolicy: Always"}); len(got) != 0 {
		t.Errorf("manifestImages() = %q, want none", got)
	}
}

// TestUpstreamCNIFlavorPaths covers the hand-encoded depth of the shared rendered manifest: a
// flavor's examples sit at <flavor>/<minor>/<version>/, so the source they refer to it by has to
// climb exactly two levels to reach the flavor root. Nothing but a cargoship create catches a
// wrong count, and that only on the flavor that has one.
func TestUpstreamCNIFlavorPaths(t *testing.T) {
	for _, f := range upstreamCNIFlavors {
		if f.manifest == "" {
			continue
		}
		if got := f.manifestFile(); got == "" || strings.Contains(got, "/") {
			t.Errorf("%s: manifestFile() = %q, want a bare file name", f.cni, got)
		}
	}

	rendered := upstreamCNIFlavor{cni: "cilium", dir: "example/upstream-cilium", chart: "1.20.2"}
	if got, want := rendered.manifestFile(), "cilium-1.20.2.yaml"; got != want {
		t.Errorf("manifestFile() = %q, want %q", got, want)
	}
	if got, want := rendered.localManifestPath(), filepath.Join("example/upstream-cilium", "cni", "cilium-1.20.2.yaml"); got != want {
		t.Errorf("localManifestPath() = %q, want %q", got, want)
	}
	if got, want := rendered.localManifestSource(), "../../cni/cilium-1.20.2.yaml"; got != want {
		t.Errorf("localManifestSource() = %q, want %q", got, want)
	}

	// The source is relative to the example directory, so joining the two has to land on the
	// path the render actually wrote.
	example := filepath.Join(rendered.dir, "v1_37", "v1.37.0")
	if got := filepath.Clean(filepath.Join(example, rendered.localManifestSource())); got != rendered.localManifestPath() {
		t.Errorf("%s resolves to %q, want %q", rendered.localManifestSource(), got, rendered.localManifestPath())
	}
}

// TestEnsureCiliumManifestExisting covers the skip that keeps helm out of CI: a manifest already
// in the repository is left alone, whether or not helm is installed.
func TestEnsureCiliumManifestExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cni", "cilium-1.20.2.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# already rendered\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ensureCiliumManifest(path, "1.20.2"); err != nil {
		t.Fatalf("ensureCiliumManifest() on an existing file: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# already rendered\n" {
		t.Errorf("file was rewritten: %q", data)
	}
}

// TestEnsureCiliumManifestNoHelm covers the error a chart pin bump produces on a host without
// helm. It says what is missing and what it was needed for, since the alternative is a render
// that writes two good flavors and a third with no CNI.
func TestEnsureCiliumManifestNoHelm(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	err := ensureCiliumManifest(filepath.Join(t.TempDir(), "cilium-9.9.9.yaml"), "9.9.9")
	if err == nil {
		t.Fatal("expected an error with no helm on PATH")
	}
	if !strings.Contains(err.Error(), "helm") || !strings.Contains(err.Error(), "9.9.9") {
		t.Errorf("error = %q, want it to name helm and the chart version", err)
	}
}

// TestRefuseEmbeddedKey covers the guard against a chart that mints a keypair while templating,
// which is what cilium's default hubble.tls.auto.method does. The base64 case is the one that
// matters: that is the form a Secret in the rendered output carries.
func TestRefuseEmbeddedKey(t *testing.T) {
	plain := "kind: Secret\ndata:\n  tls.key: |\n    -----BEGIN RSA PRIVATE KEY-----\n    aGk=\n    -----END RSA PRIVATE KEY-----\n"
	if err := refuseEmbeddedKey([]byte(plain), "1.20.2"); err == nil {
		t.Error("expected an error for a PEM key in the output")
	}

	for _, encoded := range []string{
		base64.StdEncoding.EncodeToString([]byte("-----BEGIN RSA PRIVATE KEY-----\naGk=\n")),
		base64.StdEncoding.EncodeToString([]byte("-----BEGIN PRIVATE KEY-----\naGk=\n")),
		base64.StdEncoding.EncodeToString([]byte("-----BEGIN EC PRIVATE KEY-----\naGk=\n")),
	} {
		if err := refuseEmbeddedKey([]byte("  ca.key: "+encoded+"\n"), "1.20.2"); err == nil {
			t.Errorf("expected an error for a base64 key: %s", encoded[:24])
		}
	}

	clean := "kind: DaemonSet\nspec:\n  template:\n    spec:\n      containers:\n        - image: quay.io/cilium/cilium:v1.20.2\n"
	if err := refuseEmbeddedKey([]byte(clean), "1.20.2"); err != nil {
		t.Errorf("refuseEmbeddedKey() on a key-free manifest: %v", err)
	}
}

// TestTrimTrailingSpace covers what a rendered chart arrives with: padded blank lines inside a
// block scalar, trailing spaces on a content line, and a run of newlines at the end.
func TestTrimTrailingSpace(t *testing.T) {
	got := string(trimTrailingSpace([]byte("apiVersion: v1   \ndata:\n  script: |\n    set -e\n    \n    echo hi\n\n\n")))
	want := "apiVersion: v1\ndata:\n  script: |\n    set -e\n\n    echo hi\n"
	if got != want {
		t.Errorf("trimTrailingSpace() = %q, want %q", got, want)
	}
}

func TestPruneStaleManifests(t *testing.T) {
	dir := t.TempDir()
	keep := "cilium-1.20.2.yaml"
	for _, name := range []string{keep, "cilium-1.19.5.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := pruneStaleManifests(dir, keep); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != keep {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("dir holds %q, want only %q", names, keep)
	}
}

// TestUpstreamCNIFlavorsPinned guards the two things a flavor cannot be rendered without: a
// source for its manifest, and a directory of its own to render into.
func TestUpstreamCNIFlavorsPinned(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range upstreamCNIFlavors {
		if (f.manifest == "") == (f.chart == "") {
			t.Errorf("%s: set exactly one of manifest and chart", f.cni)
		}
		if f.dir != "example/upstream-"+f.cni {
			t.Errorf("%s: dir = %q, want example/upstream-%s", f.cni, f.dir, f.cni)
		}
		if seen[f.dir] {
			t.Errorf("%s: dir %q is used twice", f.cni, f.dir)
		}
		seen[f.dir] = true
	}
}
