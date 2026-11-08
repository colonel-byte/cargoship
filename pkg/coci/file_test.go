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

package coci

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/pkg/oci"
)

// manifestWith builds a manifest carrying one layer per title. Each layer gets a real digest
// because Manifest.Locate reads one off every layer it walks.
func manifestWith(titles ...string) *oci.Manifest {
	m := &oci.Manifest{}
	for i, title := range titles {
		sum := sha256.Sum256([]byte(strconv.Itoa(i) + title))
		layer := ocispec.Descriptor{
			MediaType: "application/gzip",
			Digest:    digest.NewDigestFromEncoded(digest.SHA256, hex.EncodeToString(sum[:])),
		}
		if title != "" {
			layer.Annotations = map[string]string{ocispec.AnnotationTitle: title}
		}
		m.Layers = append(m.Layers, layer)
	}
	return m
}

func TestSelectLayer(t *testing.T) {
	for _, tc := range []struct {
		name      string
		sel       FileSelector
		manifest  *oci.Manifest
		wantTitle string
		wantErr   error
		wantIn    []string
	}{
		{
			name:      "a sole layer needs no title",
			sel:       FileSelector{},
			manifest:  manifestWith("engine.linux-amd64.tar.gz"),
			wantTitle: "engine.linux-amd64.tar.gz",
		},
		{
			name:      "a named title wins over position",
			sel:       FileSelector{Layer: "cri-o/bin/crio"},
			manifest:  manifestWith("cri-o/README.md", "cri-o/bin/crio"),
			wantTitle: "cri-o/bin/crio",
		},
		{
			name:     "several layers and no title named",
			sel:      FileSelector{},
			manifest: manifestWith("cri-o/README.md", "cri-o/bin/crio"),
			wantErr:  ErrLayerRequired,
			wantIn:   []string{"carries 2 layers", "cri-o/README.md", "cri-o/bin/crio"},
		},
		{
			name:     "a title the artifact does not carry",
			sel:      FileSelector{Layer: "cri-o/bin/conmon"},
			manifest: manifestWith("cri-o/README.md", "cri-o/bin/crio"),
			wantIn:   []string{"no layer titled", "cri-o/README.md"},
		},
		{
			name:     "an untitled sole layer is still the file",
			sel:      FileSelector{},
			manifest: manifestWith(""),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			desc, err := selectLayer("oci://registry.example.com/vendor/engine:v1", tc.sel, tc.manifest)
			if tc.wantErr == nil && len(tc.wantIn) == 0 {
				if err != nil {
					t.Fatalf("selectLayer: %v", err)
				}
				if got := desc.Annotations[ocispec.AnnotationTitle]; got != tc.wantTitle {
					t.Errorf("selected %q, want %q", got, tc.wantTitle)
				}
				return
			}
			if err == nil {
				t.Fatal("selectLayer returned no error")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("error %v does not wrap %v, so a caller cannot name the flag that fixes it", err, tc.wantErr)
			}
			for _, want := range tc.wantIn {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

func TestIndexArches(t *testing.T) {
	for _, tc := range []struct {
		name  string
		index ocispec.Index
		want  string
	}{
		{
			name: "linux children",
			index: indexOf(
				&ocispec.Platform{OS: "linux", Architecture: "arm64"},
				&ocispec.Platform{OS: "linux", Architecture: "amd64"},
			),
			want: "amd64, arm64",
		},
		{
			name: "a zarf package index, whose children say multi",
			index: indexOf(
				&ocispec.Platform{OS: "multi", Architecture: "amd64"},
				&ocispec.Platform{OS: "multi", Architecture: "skeleton"},
			),
			want: "amd64, skeleton",
		},
		{
			name: "an OS no linux node can run keeps its OS",
			index: indexOf(
				&ocispec.Platform{OS: "windows", Architecture: "amd64"},
			),
			want: "windows/amd64",
		},
		{
			name:  "a child with no platform",
			index: indexOf(nil),
			want:  "an unplatformed manifest",
		},
		{
			name:  "no children",
			index: ocispec.Index{},
			want:  "no manifests",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := indexArches(tc.index); got != tc.want {
				t.Errorf("indexArches = %q, want %q", got, tc.want)
			}
		})
	}
}

func indexOf(platforms ...*ocispec.Platform) ocispec.Index {
	var index ocispec.Index
	for _, p := range platforms {
		index.Manifests = append(index.Manifests, ocispec.Descriptor{Platform: p})
	}
	return index
}

func TestAcceptedPlatformOS(t *testing.T) {
	// A zarf package index says "multi" and a vendor artifact says "linux". Reading only one
	// of them is how ghcr.io/zarf-dev/packages/init resolved to nothing at all.
	for _, os := range []string{"", "linux", "multi"} {
		if !acceptedPlatformOS[os] {
			t.Errorf("platform OS %q is not accepted, so an artifact spelling it that way cannot be read", os)
		}
	}
	for _, os := range []string{"windows", "darwin"} {
		if acceptedPlatformOS[os] {
			t.Errorf("platform OS %q is accepted, but what is read out of these goes to a linux node", os)
		}
	}
}

func TestLayerListingChild(t *testing.T) {
	child := func(os, arch string) ocispec.Descriptor {
		return ocispec.Descriptor{Platform: &ocispec.Platform{OS: os, Architecture: arch}}
	}
	vendor := ocispec.Index{Manifests: []ocispec.Descriptor{
		child("linux", "s390x"), child("linux", "amd64"), child("linux", "arm64"),
	}}

	for _, tc := range []struct {
		name        string
		index       ocispec.Index
		arch        string
		wantArch    string
		wantAssumed bool
		wantErrIn   string
	}{
		{
			name:     "the architecture asked for",
			index:    vendor,
			arch:     "arm64",
			wantArch: "arm64",
		},
		{
			name: "no architecture asked for takes the first, and says it assumed",
			// Sorted rather than index order, so a completion suggests the same titles on
			// every invocation.
			index:       vendor,
			wantArch:    "amd64",
			wantAssumed: true,
		},
		{
			name:     "a single-child index is not an assumption",
			index:    ocispec.Index{Manifests: []ocispec.Descriptor{child("multi", "amd64")}},
			wantArch: "amd64",
		},
		{
			name:      "an architecture the index does not hold",
			index:     vendor,
			arch:      "riscv64",
			wantErrIn: "holds no riscv64 manifest",
		},
		{
			name:      "every child is for an OS a linux node cannot run",
			index:     ocispec.Index{Manifests: []ocispec.Descriptor{child("windows", "amd64")}},
			wantErrIn: "offers no manifest a linux node can use",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			desc, assumed, err := layerListingChild("oci://registry.example.com/vendor/engine:v1", tc.index, tc.arch)
			if tc.wantErrIn != "" {
				if err == nil {
					t.Fatal("layerListingChild returned no error")
				}
				if !strings.Contains(err.Error(), tc.wantErrIn) {
					t.Errorf("error %q does not mention %q", err, tc.wantErrIn)
				}
				return
			}
			if err != nil {
				t.Fatalf("layerListingChild: %v", err)
			}
			if desc.Platform.Architecture != tc.wantArch {
				t.Errorf("selected %s, want %s", desc.Platform.Architecture, tc.wantArch)
			}
			if assumed != tc.wantAssumed {
				t.Errorf("assumed = %v, want %v; the caller uses this to say it guessed", assumed, tc.wantAssumed)
			}
		})
	}
}
