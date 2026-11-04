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

package zarfpackage

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/pkg/archive"
)

const (
	testFileURL   = "https://get.rke2.io/install.sh"
	testFileBody  = "#!/bin/sh\necho rke2\n"
	testImageRef  = "registry.k8s.io/pause:3.10"
	testComponent = "rke2"
)

var specsVersioned = specs.Versioned{SchemaVersion: 2}

// packageFile is one component file to put in a fixture package: what the definition declared,
// where it is deployed to, and its bytes.
type packageFile struct {
	source      string
	destination string
	extractPath string
	content     string
}

// writeBlob stores data in the layout's content-addressed blob directory and returns the
// descriptor naming it.
func writeBlob(t *testing.T, layoutDir, mediaType string, data []byte) ocispec.Descriptor {
	t.Helper()
	sum := sha256.Sum256(data)
	hexSum := hex.EncodeToString(sum[:])
	blobDir := filepath.Join(layoutDir, "blobs", "sha256")
	if err := os.MkdirAll(blobDir, 0o755); err != nil {
		t.Fatalf("creating blob dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blobDir, hexSum), data, 0o644); err != nil {
		t.Fatalf("writing blob: %v", err)
	}
	return ocispec.Descriptor{
		MediaType: mediaType,
		Digest:    digest.NewDigestFromEncoded(digest.SHA256, hexSum),
		Size:      int64(len(data)),
	}
}

func writeJSONBlob(t *testing.T, layoutDir, mediaType string, v any) ocispec.Descriptor {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshaling blob: %v", err)
	}
	return writeBlob(t, layoutDir, mediaType, data)
}

// writeImages builds the images/ OCI layout zarf writes, with one manifest per reference,
// annotated the way zarf annotates them.
func writeImages(t *testing.T, dir string, refs []string) {
	t.Helper()
	if len(refs) == 0 {
		return
	}
	layoutDir := filepath.Join(dir, imagesDir)
	manifests := make([]ocispec.Descriptor, 0, len(refs))
	for _, ref := range refs {
		config := writeJSONBlob(t, layoutDir, ocispec.MediaTypeImageConfig, map[string]string{"ref": ref})
		layer := writeBlob(t, layoutDir, ocispec.MediaTypeImageLayerGzip, []byte("layer of "+ref))
		manifest := writeJSONBlob(t, layoutDir, ocispec.MediaTypeImageManifest, ocispec.Manifest{
			Versioned: specsVersioned,
			MediaType: ocispec.MediaTypeImageManifest,
			Config:    config,
			Layers:    []ocispec.Descriptor{layer},
		})
		// zarf sets both annotations to the reference it pulled the image under.
		manifest.Annotations = map[string]string{
			ocispec.AnnotationRefName:       ref,
			ocispec.AnnotationBaseImageName: ref,
		}
		manifests = append(manifests, manifest)
	}

	index := ocispec.Index{
		Versioned: specsVersioned,
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: manifests,
	}
	data, err := json.Marshal(index)
	if err != nil {
		t.Fatalf("marshaling index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(layoutDir, ocispec.ImageIndexFile), data, 0o644); err != nil {
		t.Fatalf("writing index: %v", err)
	}
	marker := []byte(`{"imageLayoutVersion":"` + ocispec.ImageLayoutVersion + `"}`)
	if err := os.WriteFile(filepath.Join(layoutDir, ocispec.ImageLayoutFile), marker, 0o644); err != nil {
		t.Fatalf("writing layout marker: %v", err)
	}
}

// writeComponentTar writes components/<component>.tar the way zarf does: a plain tar whose
// members are prefixed with the component name, with each file at files/<index>/<basename>.
func writeComponentTar(t *testing.T, dir, component string, files []packageFile) {
	t.Helper()
	componentsPath := filepath.Join(dir, componentsDir)
	if err := os.MkdirAll(componentsPath, 0o755); err != nil {
		t.Fatalf("creating components dir: %v", err)
	}
	out, err := os.Create(filepath.Join(componentsPath, component+".tar"))
	if err != nil {
		t.Fatalf("creating component tar: %v", err)
	}
	defer out.Close() //nolint:errcheck

	tw := tar.NewWriter(out)
	for idx, f := range files {
		member := strings.Join([]string{component, filesDir, strconv.Itoa(idx), filepath.Base(f.destination)}, "/")
		if err := tw.WriteHeader(&tar.Header{
			Name:     member,
			Mode:     0o644,
			Size:     int64(len(f.content)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatalf("writing tar header: %v", err)
		}
		if _, err := tw.Write([]byte(f.content)); err != nil {
			t.Fatalf("writing tar body: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("closing tar: %v", err)
	}
}

// writeDefinition writes the zarf.yaml a built package carries, in the given apiVersion.
//
// The component's images are deliberately left out: the reader takes images from the OCI layout
// in images/, never from zarf.yaml, and the two apiVersions spell an image list differently. A
// fixture that declared them would be asserting something the reader does not read.
func writeDefinition(t *testing.T, dir, apiVersion, component string, files []packageFile) {
	t.Helper()
	var b strings.Builder
	if apiVersion != "" {
		b.WriteString("apiVersion: " + apiVersion + "\n")
	}
	b.WriteString("kind: ZarfPackageConfig\nmetadata:\n  name: fixture\ncomponents:\n")
	b.WriteString("  - name: " + component + "\n")
	if len(files) > 0 {
		b.WriteString("    files:\n")
		for _, f := range files {
			b.WriteString("      - source: " + f.source + "\n")
			// v1beta1 renamed target to destination and shasum to checksum.
			if apiVersion == "zarf.dev/v1beta1" {
				b.WriteString("        destination: " + f.destination + "\n")
			} else {
				b.WriteString("        target: " + f.destination + "\n")
			}
			if f.extractPath != "" {
				b.WriteString("        extractPath: " + f.extractPath + "\n")
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, zarfYAML), []byte(b.String()), 0o644); err != nil {
		t.Fatalf("writing %s: %v", zarfYAML, err)
	}
}

// newPackageDir builds an unpacked fixture package and returns its directory.
func newPackageDir(t *testing.T, apiVersion string, files []packageFile, images []string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "package")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating package dir: %v", err)
	}
	writeDefinition(t, dir, apiVersion, testComponent, files)
	writeComponentTar(t, dir, testComponent, files)
	writeImages(t, dir, images)
	return dir
}

func defaultFiles() []packageFile {
	return []packageFile{{
		source:      testFileURL,
		destination: "/usr/local/bin/install.sh",
		content:     testFileBody,
	}}
}

func openDir(t *testing.T, dir string) *Package {
	t.Helper()
	p, err := OpenDir(context.Background(), dir, dir)
	if err != nil {
		t.Fatalf("OpenDir: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() }) //nolint:errcheck // the temp dir is the test runner's to lose
	return p
}

// Both apiVersions a built zarf.yaml can carry resolve to the same index, which is the whole
// point of going through the conversion boundary rather than reading one schema.
func TestOpenPackageAPIVersions(t *testing.T) {
	for _, apiVersion := range []string{"", "zarf.dev/v1alpha1", "zarf.dev/v1beta1"} {
		t.Run("apiVersion="+apiVersion, func(t *testing.T) {
			p := openDir(t, newPackageDir(t, apiVersion, defaultFiles(), []string{testImageRef}))

			name, ok := p.File(testFileURL)
			if !ok {
				t.Fatalf("File(%q) not found", testFileURL)
			}
			if name != "install.sh" {
				t.Errorf("file name %q, want install.sh", name)
			}
			if !p.HasImage(testImageRef) {
				t.Errorf("HasImage(%q) false, want true", testImageRef)
			}
		})
	}
}

func TestFetchWritesComponentFile(t *testing.T) {
	p := openDir(t, newPackageDir(t, "zarf.dev/v1alpha1", defaultFiles(), nil))

	dst := filepath.Join(t.TempDir(), "out")
	if err := p.Fetch(context.Background(), testFileURL, dst); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("reading fetched file: %v", err)
	}
	if string(got) != testFileBody {
		t.Errorf("fetched %q, want %q", got, testFileBody)
	}
}

func TestFetchUnheldFile(t *testing.T) {
	p := openDir(t, newPackageDir(t, "zarf.dev/v1alpha1", defaultFiles(), nil))

	err := p.Fetch(context.Background(), "https://example.com/absent", filepath.Join(t.TempDir(), "out"))
	if err == nil {
		t.Fatal("Fetch of a file the package does not hold returned no error")
	}
}

// A file source is a URL, not an image reference, and must not be put through the image
// canonicaliser.
func TestFileRefIsNotCanonicalised(t *testing.T) {
	p := openDir(t, newPackageDir(t, "zarf.dev/v1alpha1", defaultFiles(), nil))

	if _, ok := p.File(testFileURL); !ok {
		t.Errorf("File(%q) not found; a URL key was rewritten", testFileURL)
	}
}

// zarf stores the extracted result of an extractPath entry rather than the archive it came
// from, so the entry has to stay out of the index or the build would extract twice.
func TestExtractPathFileIsSkipped(t *testing.T) {
	files := []packageFile{{
		source:      "https://get.rke2.io/rke2.linux-amd64.tar.gz",
		destination: "/usr/local/bin/rke2",
		extractPath: "rke2",
		content:     "binary",
	}}
	p := openDir(t, newPackageDir(t, "zarf.dev/v1alpha1", files, []string{testImageRef}))

	if _, ok := p.File(files[0].source); ok {
		t.Errorf("File(%q) found; a pre-extracted file was indexed", files[0].source)
	}
}

// zarf records what it pulled under the registry's canonical host, so an image a distro
// definition declares as docker.io/... is stored as index.docker.io/..., and every spelling of
// an official image has to resolve to the one entry.
func TestDockerHubSpellingsResolve(t *testing.T) {
	p := openDir(t, newPackageDir(t, "zarf.dev/v1alpha1", nil, []string{"index.docker.io/library/nginx:1.27"}))

	for _, spelling := range []string{
		"nginx:1.27",
		"library/nginx:1.27",
		"docker.io/nginx:1.27",
		"docker.io/library/nginx:1.27",
		"index.docker.io/library/nginx:1.27",
	} {
		if !p.HasImage(spelling) {
			t.Errorf("HasImage(%q) false, want true", spelling)
		}
		if _, err := p.Target().Resolve(context.Background(), spelling); err != nil {
			t.Errorf("Resolve(%q): %v", spelling, err)
		}
	}
}

func TestResolveUnknownImage(t *testing.T) {
	p := openDir(t, newPackageDir(t, "zarf.dev/v1alpha1", nil, []string{testImageRef}))

	if _, err := p.Target().Resolve(context.Background(), "registry.k8s.io/absent:1"); err == nil {
		t.Fatal("Resolve of an image the package does not hold returned no error")
	}
}

// A package of files alone has no images directory at all, which must read as "no images"
// rather than as a broken package.
func TestFilesOnlyPackage(t *testing.T) {
	p := openDir(t, newPackageDir(t, "zarf.dev/v1alpha1", defaultFiles(), nil))

	if p.HasImage(testImageRef) {
		t.Errorf("HasImage(%q) true on a package carrying no images", testImageRef)
	}
	if exists, err := p.Target().Exists(context.Background(), ocispec.Descriptor{}); err != nil || exists {
		t.Errorf("Exists on a package carrying no images = %v, %v; want false, nil", exists, err)
	}
	if _, err := p.Target().Fetch(context.Background(), ocispec.Descriptor{}); err == nil {
		t.Error("Fetch on a package carrying no images returned no error")
	}
}

func TestOpenErrors(t *testing.T) {
	t.Run("missing path", func(t *testing.T) {
		if _, err := Open(context.Background(), filepath.Join(t.TempDir(), "absent")); err == nil {
			t.Fatal("Open of a missing path returned no error")
		}
	})

	t.Run("split package", func(t *testing.T) {
		part := filepath.Join(t.TempDir(), "zarf-package-foo-amd64.tar.zst.part000")
		if err := os.WriteFile(part, []byte("partial"), 0o644); err != nil {
			t.Fatalf("writing part: %v", err)
		}
		_, err := Open(context.Background(), part)
		if err == nil {
			t.Fatal("Open of a split part returned no error")
		}
		if !strings.Contains(err.Error(), "split zarf package") {
			t.Errorf("error %q does not say the package is split", err)
		}
	})

	t.Run("not a zarf package", func(t *testing.T) {
		_, err := OpenDir(context.Background(), t.TempDir(), "empty")
		if err == nil {
			t.Fatal("OpenDir of a directory with no zarf.yaml returned no error")
		}
		if !strings.Contains(err.Error(), zarfYAML) {
			t.Errorf("error %q does not name the missing definition", err)
		}
	})

	t.Run("unsupported apiVersion", func(t *testing.T) {
		dir := newPackageDir(t, "zarf.dev/v1gamma9", defaultFiles(), nil)
		_, err := OpenDir(context.Background(), dir, dir)
		if err == nil {
			t.Fatal("OpenDir of an unsupported apiVersion returned no error")
		}
		if !strings.Contains(err.Error(), "apiVersion") {
			t.Errorf("error %q does not name the apiVersion problem", err)
		}
	})

	t.Run("no usable content", func(t *testing.T) {
		dir := newPackageDir(t, "zarf.dev/v1alpha1", nil, nil)
		_, err := OpenDir(context.Background(), dir, dir)
		if err == nil {
			t.Fatal("OpenDir of a package with no content returned no error")
		}
		if !strings.Contains(err.Error(), ocispec.AnnotationRefName) {
			t.Errorf("error %q does not name the image annotation", err)
		}
	})
}

// Open must read a real archive, and Close must remove only what it unpacked.
func TestOpenArchive(t *testing.T) {
	ctx := context.Background()
	dir := newPackageDir(t, "zarf.dev/v1alpha1", defaultFiles(), []string{testImageRef})

	// zarf maps the package's contents to the archive root rather than nesting them under a
	// directory, so the sources are the package's own entries.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading package dir: %v", err)
	}
	sources := make([]string, 0, len(entries))
	for _, e := range entries {
		sources = append(sources, filepath.Join(dir, e.Name()))
	}

	archivePath := filepath.Join(t.TempDir(), "zarf-package-fixture-amd64.tar.zst")
	if err := archive.Compress(ctx, sources, archivePath, archive.CompressOpts{}); err != nil {
		t.Fatalf("building test archive: %v", err)
	}

	p, err := Open(ctx, archivePath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	unpacked := p.tempDir
	if unpacked == "" {
		t.Fatal("Open of an archive recorded no temporary directory")
	}
	if _, ok := p.File(testFileURL); !ok {
		t.Errorf("File(%q) not found in the archive", testFileURL)
	}
	if !p.HasImage(testImageRef) {
		t.Errorf("HasImage(%q) false, want true", testImageRef)
	}
	dst := filepath.Join(t.TempDir(), "out")
	if err := p.Fetch(ctx, testFileURL, dst); err != nil {
		t.Fatalf("Fetch from an archive: %v", err)
	}

	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(unpacked); !os.IsNotExist(err) {
		t.Errorf("Close left the unpacked archive at %s", unpacked)
	}
}

// Reading an already-extracted package must not remove it: the directory belongs to whoever
// unpacked it, which for a real build is openContentStore in pkg/distro.
func TestCloseLeavesAPackageDirectory(t *testing.T) {
	dir := newPackageDir(t, "zarf.dev/v1alpha1", defaultFiles(), nil)
	p, err := OpenDir(context.Background(), dir, dir)
	if err != nil {
		t.Fatalf("OpenDir: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, zarfYAML)); err != nil {
		t.Errorf("Close removed part of the package directory: %v", err)
	}
}

// TestRealZarfPackage checks the reader against output from the zarf CLI rather than from the
// fixtures above, which encode cargoship's understanding of the format and so cannot catch a
// change in zarf itself. Point CARGOSHIP_TEST_ZARF_PACKAGE at a built package:
//
//	zarf package create ./my-package --confirm
//	CARGOSHIP_TEST_ZARF_PACKAGE=./zarf-package-my-package-amd64.tar.zst \
//	  go test ./pkg/zarfpackage/ -run TestRealZarfPackage -v
func TestRealZarfPackage(t *testing.T) {
	path := os.Getenv("CARGOSHIP_TEST_ZARF_PACKAGE")
	if path == "" {
		t.Skip("set CARGOSHIP_TEST_ZARF_PACKAGE to a built zarf package")
	}
	p, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	defer p.Close() //nolint:errcheck

	if len(p.images) == 0 && len(p.files) == 0 {
		t.Fatal("no content indexed")
	}
	for src, entry := range p.files {
		t.Logf("file %s -> %s", src, entry.member)
		dst := filepath.Join(t.TempDir(), entry.name)
		if err := p.Fetch(context.Background(), src, dst); err != nil {
			t.Errorf("Fetch(%q): %v", src, err)
		}
	}
	for ref, desc := range p.images {
		t.Logf("image %s -> %s (%s)", ref, desc.Digest, desc.MediaType)
	}
}
