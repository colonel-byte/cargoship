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

package haulerstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/pkg/archive"

	"github.com/colonel-byte/cargoship/pkg/contentstore"
)

const (
	testFileURL  = "https://get.rke2.io/install.sh"
	testImageRef = "registry.k8s.io/pause:3.10"
)

// specsVersioned is the schema version every manifest and index in these fixtures carries.
var specsVersioned = specs.Versioned{SchemaVersion: 2}

// fileFor looks a file up naming no architecture, which is what an entry asking for a
// single-platform artifact does. The error return is only ever non-nil for a reference the
// store holds under several architectures, which these cases do not build.
func fileFor(t *testing.T, s *Store, src string) (string, bool) {
	t.Helper()
	name, ok, err := s.File(contentstore.FileRequest{Source: src})
	if err != nil {
		t.Fatalf("File(%q): %v", src, err)
	}
	return name, ok
}

func digestOf(hexSum string) digest.Digest {
	return digest.NewDigestFromEncoded(digest.SHA256, hexSum)
}

// writeBlob stores data in the layout's content-addressed blob directory, the way hauler does,
// and returns the descriptor naming it.
func writeBlob(t *testing.T, dir string, mediaType string, data []byte, annotations map[string]string) ocispec.Descriptor {
	t.Helper()
	sum := sha256.Sum256(data)
	hexSum := hex.EncodeToString(sum[:])
	blobDir := filepath.Join(dir, "blobs", "sha256")
	if err := os.MkdirAll(blobDir, 0o755); err != nil {
		t.Fatalf("creating blob dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blobDir, hexSum), data, 0o644); err != nil {
		t.Fatalf("writing blob: %v", err)
	}
	return ocispec.Descriptor{
		MediaType:   mediaType,
		Digest:      digestOf(hexSum),
		Size:        int64(len(data)),
		Annotations: annotations,
	}
}

func writeJSONBlob(t *testing.T, dir string, mediaType string, v any) ocispec.Descriptor {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshaling blob: %v", err)
	}
	return writeBlob(t, dir, mediaType, b, nil)
}

// writeIndex writes the store's index.json. A live hauler store has no oci-layout marker, so
// none is written here either -- reading one of those is a case the reader must handle.
func writeIndex(t *testing.T, dir string, manifests []ocispec.Descriptor) {
	t.Helper()
	index := ocispec.Index{
		Versioned: specsVersioned,
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: manifests,
	}
	b, err := json.Marshal(index)
	if err != nil {
		t.Fatalf("marshaling index: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating store dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ocispec.ImageIndexFile), b, 0o644); err != nil {
		t.Fatalf("writing index: %v", err)
	}
}

// fileEntry writes a hauler file artifact and returns its index descriptor.
func fileEntry(t *testing.T, dir string, originalRef string, name string, content []byte) ocispec.Descriptor {
	t.Helper()
	layer := writeBlob(t, dir, "application/vnd.content.hauler.file.layer.v1", content,
		map[string]string{ocispec.AnnotationTitle: name})
	config := writeJSONBlob(t, dir, "application/vnd.content.hauler.file.http.config.v1+json", map[string]string{})
	manifest := writeJSONBlob(t, dir, ocispec.MediaTypeImageManifest, ocispec.Manifest{
		Versioned: specsVersioned,
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    config,
		Layers:    []ocispec.Descriptor{layer},
	})
	manifest.Annotations = map[string]string{
		AnnotationOriginalRef:     originalRef,
		AnnotationKind:            "dev.hauler/image",
		ocispec.AnnotationRefName: "hauler/" + name + ":latest",
	}
	return manifest
}

// imageEntry writes a multi-architecture image the way hauler records one.
func imageEntry(t *testing.T, dir string, originalRef string) ocispec.Descriptor {
	t.Helper()
	config := writeJSONBlob(t, dir, ocispec.MediaTypeImageConfig, map[string]string{})
	manifest := writeJSONBlob(t, dir, ocispec.MediaTypeImageManifest, ocispec.Manifest{
		Versioned: specsVersioned,
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    config,
		Layers:    []ocispec.Descriptor{},
	})
	index := writeJSONBlob(t, dir, mediaTypeDockerManifestList, ocispec.Index{
		Versioned: specsVersioned,
		MediaType: mediaTypeDockerManifestList,
		Manifests: []ocispec.Descriptor{manifest},
	})
	index.Annotations = map[string]string{
		AnnotationOriginalRef:      originalRef,
		AnnotationKind:             "dev.hauler/imageIndex",
		"io.containerd.image.name": originalRef,
		ocispec.AnnotationRefName:  "pause:3.10",
	}
	return index
}

// sigsEntry writes a cosign signature. It carries the same original ref as the image it signs
// and an OCI image config, so nothing but its kind distinguishes it from that image.
func sigsEntry(t *testing.T, dir string, originalRef string, subject string) ocispec.Descriptor {
	t.Helper()
	layer := writeBlob(t, dir, "application/vnd.dev.cosign.simplesigning.v1+json", []byte(`{}`), nil)
	config := writeJSONBlob(t, dir, ocispec.MediaTypeImageConfig, map[string]string{})
	manifest := writeJSONBlob(t, dir, ocispec.MediaTypeImageManifest, ocispec.Manifest{
		Versioned: specsVersioned,
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    config,
		Layers:    []ocispec.Descriptor{layer},
	})
	manifest.Annotations = map[string]string{
		AnnotationOriginalRef:     originalRef,
		AnnotationKind:            "dev.hauler/sigs",
		AnnotationSubjectDigest:   subject,
		ocispec.AnnotationRefName: "pause:3.10",
	}
	return manifest
}

// newStoreDir builds a store holding one file, one image, and a signature for that image.
func newStoreDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "store")
	content, err := os.ReadFile(filepath.Join("testdata", "install.sh"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	file := fileEntry(t, dir, testFileURL, "install.sh", content)
	image := imageEntry(t, dir, testImageRef)
	sigs := sigsEntry(t, dir, testImageRef, image.Digest.String())
	writeIndex(t, dir, []ocispec.Descriptor{file, image, sigs})
	return dir
}

func TestOpenStoreDirectory(t *testing.T) {
	s, err := Open(context.Background(), newStoreDir(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close() //nolint:errcheck

	name, ok := fileFor(t, s, testFileURL)
	if !ok {
		t.Fatalf("File(%q) not found", testFileURL)
	}
	if name != "install.sh" {
		t.Errorf("file name %q, want install.sh", name)
	}
	if !s.HasImage(testImageRef) {
		t.Errorf("HasImage(%q) false, want true", testImageRef)
	}
}

// A cosign signature carries the same original ref as its image. Indexing it as content would
// replace the image with the signature, and the build would copy the wrong bytes.
func TestSignatureDoesNotShadowImage(t *testing.T) {
	dir := newStoreDir(t)
	s, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close() //nolint:errcheck

	desc, err := s.Target().Resolve(context.Background(), testImageRef)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if desc.MediaType != mediaTypeDockerManifestList {
		t.Errorf("resolved media type %q, want the image index %q -- a signature was indexed over the image",
			desc.MediaType, mediaTypeDockerManifestList)
	}
	if desc.Annotations[AnnotationKind] != "dev.hauler/imageIndex" {
		t.Errorf("resolved kind %q, want dev.hauler/imageIndex", desc.Annotations[AnnotationKind])
	}
}

// Reading must not write to the operator's store, which has no oci-layout marker of its own.
func TestOpenDoesNotModifyStore(t *testing.T) {
	dir := newStoreDir(t)
	before := snapshot(t, dir)

	s, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if after := snapshot(t, dir); after != before {
		t.Errorf("store changed on open:\nbefore %s\nafter  %s", before, after)
	}
	if _, err := os.Stat(filepath.Join(dir, ocispec.ImageLayoutFile)); !os.IsNotExist(err) {
		t.Errorf("an %s marker was created in the store", ocispec.ImageLayoutFile)
	}
}

// An artifact stored under an image config but carrying a layer title is file content, which is
// how hauler holds release binaries.
func TestImageConfigWithTitledLayerIsAFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "store")
	layer := writeBlob(t, dir, ocispec.MediaTypeImageLayerGzip, []byte("binary"),
		map[string]string{ocispec.AnnotationTitle: "rke2.linux-amd64"})
	config := writeJSONBlob(t, dir, ocispec.MediaTypeImageConfig, map[string]string{})
	manifest := writeJSONBlob(t, dir, ocispec.MediaTypeImageManifest, ocispec.Manifest{
		Versioned: specsVersioned,
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    config,
		Layers:    []ocispec.Descriptor{layer},
	})
	manifest.Annotations = map[string]string{
		AnnotationOriginalRef: "https://get.rke2.io/rke2.linux-amd64",
		AnnotationKind:        "dev.hauler/image",
	}
	writeIndex(t, dir, []ocispec.Descriptor{manifest})

	s, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close() //nolint:errcheck

	name, ok := fileFor(t, s, "https://get.rke2.io/rke2.linux-amd64")
	if !ok {
		t.Fatal("artifact with an image config and a titled layer was not indexed as a file")
	}
	if name != "rke2.linux-amd64" {
		t.Errorf("file name %q, want rke2.linux-amd64", name)
	}
}

func TestFetchWritesContent(t *testing.T) {
	s, err := Open(context.Background(), newStoreDir(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close() //nolint:errcheck

	if _, ok := fileFor(t, s, testFileURL); !ok {
		t.Fatalf("File(%q) not found", testFileURL)
	}
	dst := filepath.Join(t.TempDir(), "out")
	if err := s.Fetch(context.Background(), contentstore.FileRequest{Source: testFileURL}, dst); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("reading fetched file: %v", err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "install.sh"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("fetched %q, want %q", got, want)
	}
}

func TestOpenErrors(t *testing.T) {
	t.Run("missing path", func(t *testing.T) {
		if _, err := Open(context.Background(), filepath.Join(t.TempDir(), "absent")); err == nil {
			t.Fatal("Open of a missing path returned no error")
		}
	})

	t.Run("chunked archive", func(t *testing.T) {
		dir := t.TempDir()
		chunk := filepath.Join(dir, "haul.tar.zst.001")
		if err := os.WriteFile(chunk, []byte("partial"), 0o644); err != nil {
			t.Fatalf("writing chunk: %v", err)
		}
		_, err := Open(context.Background(), chunk)
		if err == nil {
			t.Fatal("Open of a chunk returned no error")
		}
		if !strings.Contains(err.Error(), "hauler store load") {
			t.Errorf("error %q does not say how to rejoin the chunks", err)
		}
	})

	t.Run("not a store", func(t *testing.T) {
		if _, err := Open(context.Background(), t.TempDir()); err == nil {
			t.Fatal("Open of a directory with no index returned no error")
		}
	})

	t.Run("no original refs", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "store")
		config := writeJSONBlob(t, dir, ocispec.MediaTypeImageConfig, map[string]string{})
		manifest := writeJSONBlob(t, dir, ocispec.MediaTypeImageManifest, ocispec.Manifest{
			Versioned: specsVersioned,
			MediaType: ocispec.MediaTypeImageManifest,
			Config:    config,
		})
		writeIndex(t, dir, []ocispec.Descriptor{manifest})

		_, err := Open(context.Background(), dir)
		if err == nil {
			t.Fatal("Open of a layout with no original refs returned no error")
		}
		if !strings.Contains(err.Error(), AnnotationOriginalRef) {
			t.Errorf("error %q does not name the missing annotation", err)
		}
	})
}

func TestResolveUnknownImage(t *testing.T) {
	s, err := Open(context.Background(), newStoreDir(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close() //nolint:errcheck

	if _, err := s.Target().Resolve(context.Background(), "example.com/absent:v1"); err == nil {
		t.Fatal("Resolve of an absent image returned no error")
	}
	if s.HasImage("example.com/absent:v1") {
		t.Error("HasImage true for an absent image")
	}
}

func snapshot(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		h.Write([]byte(rel))
		if info.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		h.Write(b)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshotting %s: %v", dir, err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// An archive is what `hauler store save` produces, and unlike a live store it carries its own
// oci-layout marker. Open must unpack it and index it the same way, and Close must take the
// unpacked copy away again.
func TestOpenArchive(t *testing.T) {
	ctx := context.Background()
	dir := newStoreDir(t)

	// Hauler maps the store's contents to the archive root rather than nesting them under a
	// directory, so the sources are the store's entries.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading store dir: %v", err)
	}
	sources := make([]string, 0, len(entries)+1)
	for _, e := range entries {
		sources = append(sources, filepath.Join(dir, e.Name()))
	}
	// A saved haul carries the marker a live store lacks. Written out here rather than taken
	// from the reader, so that this exercises a real archive rather than whatever the reader
	// would have served itself.
	marker := filepath.Join(dir, ocispec.ImageLayoutFile)
	markerContent := []byte(`{"imageLayoutVersion":"` + ocispec.ImageLayoutVersion + `"}`)
	if err := os.WriteFile(marker, markerContent, 0o644); err != nil {
		t.Fatalf("writing layout marker: %v", err)
	}
	sources = append(sources, marker)

	archivePath := filepath.Join(t.TempDir(), "haul.tar.zst")
	if err := archive.Compress(ctx, sources, archivePath, archive.CompressOpts{}); err != nil {
		t.Fatalf("building test archive: %v", err)
	}

	s, err := Open(ctx, archivePath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	unpacked := s.tempDir
	if unpacked == "" {
		t.Fatal("Open of an archive left tempDir empty, so Close would remove nothing")
	}

	if _, ok := fileFor(t, s, testFileURL); !ok {
		t.Errorf("File(%q) not found in the archive", testFileURL)
	}
	if !s.HasImage(testImageRef) {
		t.Errorf("HasImage(%q) false, want true", testImageRef)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(unpacked); !os.IsNotExist(err) {
		t.Errorf("Close left the unpacked archive at %s", unpacked)
	}
}

// TestRealHaul checks the reader against output from the hauler CLI rather than from the
// fixtures above, which encode cargoship's understanding of the format and so cannot catch a
// change in hauler itself. Point CARGOSHIP_TEST_HAUL at a store directory or an archive:
//
//	hauler store add file https://get.rke2.io/install.sh -s /tmp/h/store
//	hauler store add image registry.k8s.io/pause:3.10 -s /tmp/h/store
//	CARGOSHIP_TEST_HAUL=/tmp/h/store go test ./pkg/haulerstore/ -run TestRealHaul -v
func TestRealHaul(t *testing.T) {
	path := os.Getenv("CARGOSHIP_TEST_HAUL")
	if path == "" {
		t.Skip("set CARGOSHIP_TEST_HAUL to a hauler store directory or archive")
	}
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	defer s.Close() //nolint:errcheck

	if len(s.images) == 0 && len(s.files) == 0 {
		t.Fatal("no content indexed")
	}
	for ref, entry := range s.files {
		t.Logf("file %s -> %s", ref, entry.name)
	}
	for ref, desc := range s.images {
		t.Logf("image %s -> %s (%s)", ref, desc.Digest, desc.MediaType)
		if desc.Annotations[AnnotationKind] == "dev.hauler/sigs" {
			t.Errorf("image %s resolved to a signature", ref)
		}
	}
}

// Hauler records a Docker Hub image under the registry's canonical host, so an image a distro
// definition declares as docker.io/... is stored as index.docker.io/..., and an exact-string
// match would miss every one of them. Both spellings have to resolve.
func TestDockerHubSpellingsResolve(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "store")
	// index.docker.io is what hauler writes; docker.io is what a definition declares.
	image := imageEntry(t, dir, "index.docker.io/rancher/mirrored-pause:3.10.2")
	writeIndex(t, dir, []ocispec.Descriptor{image})

	s, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close() //nolint:errcheck

	for _, ref := range []string{
		"docker.io/rancher/mirrored-pause:3.10.2",
		"index.docker.io/rancher/mirrored-pause:3.10.2",
	} {
		if !s.HasImage(ref) {
			t.Errorf("HasImage(%q) false, want true", ref)
		}
		if _, err := s.Target().Resolve(context.Background(), ref); err != nil {
			t.Errorf("Resolve(%q): %v", ref, err)
		}
	}

	// An official image is the same reference written three ways.
	official := filepath.Join(t.TempDir(), "official")
	writeIndex(t, official, []ocispec.Descriptor{imageEntry(t, official, "index.docker.io/library/nginx:1.0")})
	o, err := Open(context.Background(), official)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer o.Close() //nolint:errcheck
	for _, ref := range []string{"nginx:1.0", "docker.io/nginx:1.0", "docker.io/library/nginx:1.0"} {
		if !o.HasImage(ref) {
			t.Errorf("HasImage(%q) false, want true", ref)
		}
	}
}

// A file's recorded origin is a URL, not an image reference, and must not be put through the
// image canonicaliser.
func TestFileRefIsNotCanonicalised(t *testing.T) {
	s, err := Open(context.Background(), newStoreDir(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close() //nolint:errcheck

	if _, ok := fileFor(t, s, testFileURL); !ok {
		t.Errorf("File(%q) not found; a URL key was rewritten", testFileURL)
	}
}

// multiPlatformFileEntry writes a file artifact published as an image index over one manifest
// per platform, which is how at least one vendor ships an air-gapped engine bundle: one tag,
// an index, and one titled layer per architecture.
func multiPlatformFileEntry(t *testing.T, dir string, originalRef string, name string, perArch map[string][]byte) ocispec.Descriptor {
	t.Helper()
	children := make([]ocispec.Descriptor, 0, len(perArch))
	for _, arch := range slices.Sorted(maps.Keys(perArch)) {
		child := fileEntry(t, dir, originalRef, name+".linux-"+arch, perArch[arch])
		child.Annotations = nil
		child.Platform = &ocispec.Platform{OS: "linux", Architecture: arch}
		children = append(children, child)
	}
	index := writeJSONBlob(t, dir, ocispec.MediaTypeImageIndex, ocispec.Index{
		Versioned: specsVersioned,
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: children,
	})
	index.Annotations = map[string]string{
		AnnotationOriginalRef:     originalRef,
		AnnotationKind:            "dev.hauler/image",
		ocispec.AnnotationRefName: "hauler/" + name + ":latest",
	}
	return index
}

const multiPlatformRef = "oci://registry.example.com/vendor/engine:v1.37.1"

// newMultiPlatformStoreDir builds a store holding one file artifact published as an index over
// an amd64 and an arm64 child.
func newMultiPlatformStoreDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "store")
	entry := multiPlatformFileEntry(t, dir, multiPlatformRef, "engine", map[string][]byte{
		"amd64": []byte("amd64 engine"),
		"arm64": []byte("arm64 engine"),
	})
	writeIndex(t, dir, []ocispec.Descriptor{entry})
	return dir
}

// An index was taken to be an image for as long as this reader existed, so a store holding a
// vendor-published file artifact resolved nothing at all. The entry's own architecture is what
// picks the child out of it.
func TestMultiPlatformFileResolvesPerArch(t *testing.T) {
	s, err := Open(context.Background(), newMultiPlatformStoreDir(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close() //nolint:errcheck

	for arch, want := range map[string]string{"amd64": "amd64 engine", "arm64": "arm64 engine"} {
		req := contentstore.FileRequest{Source: multiPlatformRef, Arch: arch}
		name, ok, err := s.File(req)
		if err != nil {
			t.Fatalf("File(%s): %v", arch, err)
		}
		if !ok {
			t.Fatalf("File(%s) not found; the index was not descended", arch)
		}
		if name != "engine.linux-"+arch {
			t.Errorf("file name %q, want engine.linux-%s", name, arch)
		}

		dst := filepath.Join(t.TempDir(), name)
		if err := s.Fetch(context.Background(), req, dst); err != nil {
			t.Fatalf("Fetch(%s): %v", arch, err)
		}
		got, err := os.ReadFile(dst)
		if err != nil {
			t.Fatalf("reading fetched file: %v", err)
		}
		if string(got) != want {
			t.Errorf("fetched %q, want %q; the wrong platform child was resolved", got, want)
		}
	}
}

// An entry whose source holds several platforms and whose selector names no single architecture
// is genuinely ambiguous. Refuse it and name the entry, rather than reaching the network for
// content the operator staged or shipping a binary the node cannot run.
func TestMultiPlatformFileWithoutArchIsRefused(t *testing.T) {
	s, err := Open(context.Background(), newMultiPlatformStoreDir(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close() //nolint:errcheck

	_, ok, err := s.File(contentstore.FileRequest{Source: multiPlatformRef})
	if err == nil {
		t.Fatal("File with no architecture returned no error for a multi-platform artifact")
	}
	if ok {
		t.Error("File reported the ambiguous reference as held")
	}
	for _, want := range []string{multiPlatformRef, "amd64", "arm64", "selector.arch"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}

	if err := s.Fetch(context.Background(), contentstore.FileRequest{Source: multiPlatformRef}, filepath.Join(t.TempDir(), "out")); err == nil {
		t.Error("Fetch of the ambiguous reference returned no error")
	}
}

// A store synced for two architectures and asked for a third is a partial store, which is the
// case the build already handles by falling back to the declared source. It is a miss, not an
// error.
func TestMultiPlatformFileOtherArchIsAMiss(t *testing.T) {
	s, err := Open(context.Background(), newMultiPlatformStoreDir(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close() //nolint:errcheck

	_, ok, err := s.File(contentstore.FileRequest{Source: multiPlatformRef, Arch: "s390x"})
	if err != nil {
		t.Fatalf("File for an architecture the store does not hold: %v", err)
	}
	if ok {
		t.Error("File reported an architecture the store does not hold as held")
	}
}

// A single-platform file artifact carries no platform and answers for every architecture, which
// is what every store written before an index was descended holds.
func TestSinglePlatformFileAnswersForAnyArch(t *testing.T) {
	s, err := Open(context.Background(), newStoreDir(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close() //nolint:errcheck

	name, ok, err := s.File(contentstore.FileRequest{Source: testFileURL, Arch: "arm64"})
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	if !ok {
		t.Fatalf("File(%q) not found when an architecture was named", testFileURL)
	}
	if name != "install.sh" {
		t.Errorf("file name %q, want install.sh", name)
	}
}

// An index whose children are part file and part something else has no reading better than a
// guess, and guessing would silently ship the wrong bytes.
func TestMixedIndexIsRefused(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "store")
	file := fileEntry(t, dir, multiPlatformRef, "engine.linux-amd64", []byte("amd64 engine"))
	file.Annotations = nil
	file.Platform = &ocispec.Platform{OS: "linux", Architecture: "amd64"}
	plain := writeJSONBlob(t, dir, ocispec.MediaTypeImageManifest, ocispec.Manifest{
		Versioned: specsVersioned,
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    writeJSONBlob(t, dir, ocispec.MediaTypeImageConfig, map[string]string{}),
		Layers:    []ocispec.Descriptor{},
	})
	plain.Platform = &ocispec.Platform{OS: "linux", Architecture: "arm64"}
	index := writeJSONBlob(t, dir, ocispec.MediaTypeImageIndex, ocispec.Index{
		Versioned: specsVersioned,
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: []ocispec.Descriptor{file, plain},
	})
	index.Annotations = map[string]string{
		AnnotationOriginalRef: multiPlatformRef,
		AnnotationKind:        "dev.hauler/image",
	}
	writeIndex(t, dir, []ocispec.Descriptor{index})

	_, err := Open(context.Background(), dir)
	if err == nil {
		t.Fatal("Open of a mixed index returned no error")
	}
	if !strings.Contains(err.Error(), "only 1 of which are file artifacts") {
		t.Errorf("error %q does not name the mixed index as the reason", err)
	}
}

// bareFileRef is a registry reference as a bundle records one: no scheme, because that is what
// a registry client was handed.
const bareFileRef = "registry.example.com/vendor/engine:v1.37.1"

// A definition names an artifact `oci://registry/repo:tag`; hauler records the same artifact
// as `registry/repo:tag`, because that is what it handed a registry client. The two have to
// resolve to one entry or a store holding the file reports it as not held - which is what
// `--content-store` did for every oci:// entry until the two spellings were reconciled.
func TestOCIReferenceMatchesTheBareRefAStoreRecords(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "store")
	entry := multiPlatformFileEntry(t, dir, bareFileRef, "engine", map[string][]byte{
		"amd64": []byte("amd64 engine"),
		"arm64": []byte("arm64 engine"),
	})
	writeIndex(t, dir, []ocispec.Descriptor{entry})

	s, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close() //nolint:errcheck

	for _, src := range []string{"oci://" + bareFileRef, bareFileRef} {
		t.Run(src, func(t *testing.T) {
			name, ok, err := s.File(contentstore.FileRequest{Source: src, Arch: "amd64"})
			if err != nil {
				t.Fatalf("File: %v", err)
			}
			if !ok {
				t.Fatalf("File(%q) not held; the scheme decided the lookup", src)
			}
			if name != "engine.linux-amd64" {
				t.Errorf("file name %q, want engine.linux-amd64", name)
			}

			dst := filepath.Join(t.TempDir(), name)
			if err := s.Fetch(context.Background(), contentstore.FileRequest{Source: src, Arch: "amd64"}, dst); err != nil {
				t.Fatalf("Fetch: %v", err)
			}
			got, err := os.ReadFile(dst)
			if err != nil {
				t.Fatalf("reading fetched file: %v", err)
			}
			if string(got) != "amd64 engine" {
				t.Errorf("fetched %q, want the amd64 child", got)
			}
		})
	}
}

// The refusal an ambiguous entry gets must name the architectures too, whichever spelling the
// definition used to ask.
func TestOCIReferenceAmbiguityIsReportedForEitherSpelling(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "store")
	entry := multiPlatformFileEntry(t, dir, bareFileRef, "engine", map[string][]byte{
		"amd64": []byte("amd64 engine"),
		"arm64": []byte("arm64 engine"),
	})
	writeIndex(t, dir, []ocispec.Descriptor{entry})

	s, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close() //nolint:errcheck

	_, _, err = s.File(contentstore.FileRequest{Source: "oci://" + bareFileRef})
	if err == nil {
		t.Fatal("an entry selecting no architecture was not refused")
	}
	for _, want := range []string{"amd64", "arm64", "selector.arch"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
