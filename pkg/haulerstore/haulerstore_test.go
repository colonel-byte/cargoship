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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/pkg/archive"
)

const (
	testFileURL  = "https://get.rke2.io/install.sh"
	testImageRef = "registry.k8s.io/pause:3.10"
)

// specsVersioned is the schema version every manifest and index in these fixtures carries.
var specsVersioned = specs.Versioned{SchemaVersion: 2}

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

	file, ok := s.File(testFileURL)
	if !ok {
		t.Fatalf("File(%q) not found", testFileURL)
	}
	if file.Name != "install.sh" {
		t.Errorf("file name %q, want install.sh", file.Name)
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

	file, ok := s.File("https://get.rke2.io/rke2.linux-amd64")
	if !ok {
		t.Fatal("artifact with an image config and a titled layer was not indexed as a file")
	}
	if file.Name != "rke2.linux-amd64" {
		t.Errorf("file name %q, want rke2.linux-amd64", file.Name)
	}
}

func TestFetchWritesContent(t *testing.T) {
	s, err := Open(context.Background(), newStoreDir(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close() //nolint:errcheck

	file, ok := s.File(testFileURL)
	if !ok {
		t.Fatalf("File(%q) not found", testFileURL)
	}
	dst := filepath.Join(t.TempDir(), "out")
	if err := s.Fetch(context.Background(), file.Descriptor, dst); err != nil {
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
	// A saved haul carries the marker a live store lacks.
	marker := filepath.Join(dir, ocispec.ImageLayoutFile)
	if err := os.WriteFile(marker, layoutMarker, 0o644); err != nil {
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

	if _, ok := s.File(testFileURL); !ok {
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
	for ref, file := range s.files {
		t.Logf("file %s -> %s", ref, file.Name)
	}
	for ref, desc := range s.images {
		t.Logf("image %s -> %s (%s)", ref, desc.Digest, desc.MediaType)
		if desc.Annotations[AnnotationKind] == "dev.hauler/sigs" {
			t.Errorf("image %s resolved to a signature", ref)
		}
	}
}
