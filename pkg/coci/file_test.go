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
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/test"
	"github.com/klauspost/compress/zstd"
	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/pkg/oci"
	"github.com/zarf-dev/zarf/src/types"
	"oras.land/oras-go/v2/registry/remote"
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

// pushTarGzArtifact pushes a single-layer artifact whose layer is a gzipped tar, titled so the
// extraction can identify its format.
func pushTarGzArtifact(t *testing.T, addr, repoName, tag, title string, members map[string]string) {
	t.Helper()
	pushTarArtifact(t, addr, repoName, tag, title, members, func(w io.Writer) io.WriteCloser {
		return gzip.NewWriter(w)
	})
}

// pushTarZstdArtifact is pushTarGzArtifact with zstd compression, for the format the package
// layout itself uses.
func pushTarZstdArtifact(t *testing.T, addr, repoName, tag, title string, members map[string]string) {
	t.Helper()
	pushTarArtifact(t, addr, repoName, tag, title, members, func(w io.Writer) io.WriteCloser {
		enc, err := zstd.NewWriter(w)
		if err != nil {
			t.Fatalf("zstd writer: %v", err)
		}
		return enc
	})
}

// pushTarArtifact pushes a single-layer artifact whose layer is a tar compressed by compress,
// titled so the extraction can identify its format from the name.
func pushTarArtifact(t *testing.T, addr, repoName, tag, title string, members map[string]string, compress func(io.Writer) io.WriteCloser) {
	t.Helper()
	ctx := context.Background()

	var buf bytes.Buffer
	gw := compress(&buf)
	tw := tar.NewWriter(gw)
	for _, name := range slices.Sorted(maps.Keys(members)) {
		body := members[name]
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body))}); err != nil {
			t.Fatalf("tar header: %v", err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatalf("tar write: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("compressor close: %v", err)
	}

	repo, err := remote.NewRepository(addr + "/" + repoName)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	repo.PlainHTTP = true

	push := func(mediaType string, b []byte, annotations map[string]string) ocispec.Descriptor {
		sum := sha256.Sum256(b)
		desc := ocispec.Descriptor{
			MediaType:   mediaType,
			Digest:      digest.NewDigestFromEncoded(digest.SHA256, hex.EncodeToString(sum[:])),
			Size:        int64(len(b)),
			Annotations: annotations,
		}
		if err := repo.Push(ctx, desc, bytes.NewReader(b)); err != nil {
			t.Fatalf("push: %v", err)
		}
		return desc
	}

	layer := push("application/gzip", buf.Bytes(), map[string]string{ocispec.AnnotationTitle: title})
	cfg := push(ocispec.MediaTypeImageConfig, []byte(`{}`), nil)
	manifestBytes, err := json.Marshal(ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    cfg,
		Layers:    []ocispec.Descriptor{layer},
	})
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	sum := sha256.Sum256(manifestBytes)
	desc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    digest.NewDigestFromEncoded(digest.SHA256, hex.EncodeToString(sum[:])),
		Size:      int64(len(manifestBytes)),
	}
	if err := repo.PushReference(ctx, desc, bytes.NewReader(manifestBytes), tag); err != nil {
		t.Fatalf("push manifest: %v", err)
	}
}

// FetchFile with an extract path stages the layer and pulls one member out of it. This is the
// path only `dev sha256sum` takes -- a package build goes through the shared extractor -- so
// without a test here it is covered by the e2e walk alone, which is where a double Close on
// the layer body went unnoticed until it reached the registry.
func TestFetchFileExtractPath(t *testing.T) {
	addr := test.SetupInMemoryRegistry(t)
	pushTarGzArtifact(t, addr, "vendor/engine", "v1", "engine.linux-amd64.tar.gz", map[string]string{
		"bin/engine":     "engine binary\n",
		"lib/engine.svc": "unit file\n",
	})
	ref := "oci://" + addr + "/vendor/engine:v1"
	sel := FileSelector{ExtractPath: "bin/engine"}
	opts := FileOptions{RemoteOptions: types.RemoteOptions{PlainHTTP: true}}

	dst := filepath.Join(t.TempDir(), "engine")
	if err := FetchFile(context.Background(), ref, sel, dst, opts); err != nil {
		t.Fatalf("FetchFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("reading staged member: %v", err)
	}
	if string(got) != "engine binary\n" {
		t.Errorf("staged %q, want the bin/engine member", got)
	}

	// Called twice, because the first call populates the layer cache and the second reads it
	// back -- the arrangement the double Close showed up in.
	second := filepath.Join(t.TempDir(), "engine")
	if err := FetchFile(context.Background(), ref, sel, second, opts); err != nil {
		t.Fatalf("FetchFile on a second call: %v", err)
	}
}

// Without an extract path the layer's own bytes are the file.
func TestFetchFileWholeLayer(t *testing.T) {
	addr := test.SetupInMemoryRegistry(t)
	pushTarGzArtifact(t, addr, "vendor/engine", "v1", "engine.linux-amd64.tar.gz", map[string]string{
		"bin/engine": "engine binary\n",
	})

	dst := filepath.Join(t.TempDir(), "engine.tar.gz")
	err := FetchFile(context.Background(), "oci://"+addr+"/vendor/engine:v1", FileSelector{}, dst,
		FileOptions{RemoteOptions: types.RemoteOptions{PlainHTTP: true}})
	if err != nil {
		t.Fatalf("FetchFile: %v", err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() == 0 {
		t.Error("the staged layer is empty")
	}
}

// The format is identified from the layer's title, so every compression the extractor
// registers has to survive being staged under that name. zstd is the one that matters most
// here: it is what a package layout itself is written in, so an artifact republishing one
// arrives as `.tar.zst` rather than `.tar.gz`.
func TestFetchFileExtractPathZstd(t *testing.T) {
	addr := test.SetupInMemoryRegistry(t)
	pushTarZstdArtifact(t, addr, "vendor/engine", "v1", "engine.linux-amd64.tar.zst", map[string]string{
		"bin/engine":     "engine binary\n",
		"lib/engine.svc": "unit file\n",
	})
	ref := "oci://" + addr + "/vendor/engine:v1"
	opts := FileOptions{RemoteOptions: types.RemoteOptions{PlainHTTP: true}}

	dst := filepath.Join(t.TempDir(), "engine")
	if err := FetchFile(context.Background(), ref, FileSelector{ExtractPath: "bin/engine"}, dst, opts); err != nil {
		t.Fatalf("FetchFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("reading staged member: %v", err)
	}
	if string(got) != "engine binary\n" {
		t.Errorf("staged %q, want the bin/engine member", got)
	}

	// A second member out of the same artifact, to pin that the title is what drives the
	// format rather than anything cached from the first read.
	other := filepath.Join(t.TempDir(), "engine.svc")
	if err := FetchFile(context.Background(), ref, FileSelector{ExtractPath: "lib/engine.svc"}, other, opts); err != nil {
		t.Fatalf("FetchFile for a second member: %v", err)
	}
	gotOther, err := os.ReadFile(other)
	if err != nil {
		t.Fatalf("reading the second member: %v", err)
	}
	if string(gotOther) != "unit file\n" {
		t.Errorf("staged %q, want the lib/engine.svc member", gotOther)
	}
}

// A layer staged under a name the extractor does not recognise cannot be opened, because the
// format is resolved from the name and not from the bytes. The failure names the archive
// rather than the temporary path it was staged at.
func TestFetchFileExtractPathUnknownExtension(t *testing.T) {
	addr := test.SetupInMemoryRegistry(t)
	pushTarZstdArtifact(t, addr, "vendor/engine", "v1", "engine.linux-amd64.bin", map[string]string{
		"bin/engine": "engine binary\n",
	})

	err := FetchFile(context.Background(), "oci://"+addr+"/vendor/engine:v1",
		FileSelector{ExtractPath: "bin/engine"}, filepath.Join(t.TempDir(), "engine"),
		FileOptions{RemoteOptions: types.RemoteOptions{PlainHTTP: true}})
	if err == nil {
		t.Fatal("a layer titled .bin was extracted from, which the extractor cannot identify")
	}
	if !strings.Contains(err.Error(), "engine.linux-amd64.bin") {
		t.Errorf("error %q does not name the archive it could not open", err)
	}
}
