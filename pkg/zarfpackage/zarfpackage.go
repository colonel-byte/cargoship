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

// Package zarfpackage reads a built zarf package (https://github.com/zarf-dev/zarf) as a source
// of images and files for a package build.
//
// Both halves of a zarf package record where their content came from, which is what makes
// resolving a distro definition against one a map lookup rather than a heuristic:
//
//   - images/ is a plain OCI image layout whose index entries carry
//     org.opencontainers.image.ref.name, set to the reference zarf pulled the image under before
//     any registry override was applied.
//   - zarf.yaml keeps each component file's source exactly as the package author wrote it, and
//     the bytes sit at a path derived from the file's position in the component. zarf rewrites
//     files[].source only when building a skeleton, which is not a package anybody deploys.
//
// Those are the same strings a distro definition writes in imageConfig.images and File.Source.
package zarfpackage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"

	goyaml "github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	zarfapi "github.com/zarf-dev/zarf/src/api"
	"github.com/zarf-dev/zarf/src/api/convert"
	"github.com/zarf-dev/zarf/src/api/v1alpha1" //nolint:staticcheck // SA1019: v1alpha1 is load-bearing here; see docs/dev/zarf-v1alpha1-surface.md
	"github.com/zarf-dev/zarf/src/api/v1beta1"
	"github.com/zarf-dev/zarf/src/pkg/archive"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"oras.land/oras-go/v2"
	orasoci "oras.land/oras-go/v2/content/oci"

	"github.com/colonel-byte/cargoship/pkg/contentstore"
)

// The parts of a zarf package layout this reader needs. These are string constants upstream
// (zarf/src/pkg/packager/layout), but that package is not vendored here and links zarf's own
// image, signing and config trees, which is a large amount of binary for six names.
const (
	zarfYAML      = "zarf.yaml"
	imagesDir     = "images"
	componentsDir = "components"
	filesDir      = "files"
)

// file locates one component file's bytes inside the unpacked package.
type file struct {
	// member is the path of the file's entry inside its component tarball, which carries the
	// component name as its first element.
	member string
	// name is the basename the file is restored under.
	name string
	// tarball is the component archive holding it, relative to the package root.
	tarball string
}

// Package is an opened zarf package. Close removes what Open unpacked, so every Package must be
// closed.
type Package struct {
	// source is the path the caller passed, used in messages and recorded provenance.
	source string
	// dir is the directory the package is read from.
	dir string
	// tempDir is the directory an archive was unpacked into, and is empty for a package
	// directory, which is never written to or removed.
	tempDir string
	store   *orasoci.ReadOnlyStore
	images  map[string]ocispec.Descriptor
	files   map[string]file
}

// Open reads the zarf package at path, which may be a built package archive or a directory it
// has already been extracted into. An archive is unpacked into a temporary directory owned by
// the returned Package.
func Open(ctx context.Context, path string) (*Package, error) {
	dir, tempDir, err := contentstore.Unpack(ctx, path)
	if err != nil {
		return nil, err
	}
	p, err := OpenDir(ctx, dir, path)
	if err != nil {
		return nil, errors.Join(err, os.RemoveAll(tempDir))
	}
	p.tempDir = tempDir
	return p, nil
}

// OpenDir reads an already-unpacked zarf package at dir, naming it in messages and recorded
// provenance as source. The caller keeps ownership of dir: the returned Package's Close does
// not remove it.
//
// This is the entry point for a caller that has unpacked a bundle itself in order to work out
// which format it is -- see openContentStore in pkg/distro.
func OpenDir(ctx context.Context, dir, source string) (*Package, error) {
	p := &Package{source: source, dir: dir}
	if err := p.load(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

// load indexes the unpacked package by the sources its definition declared.
func (p *Package) load(ctx context.Context) error {
	definition, err := p.readDefinition(ctx)
	if err != nil {
		return err
	}
	p.loadFiles(ctx, definition)
	if err := p.loadImages(ctx); err != nil {
		return err
	}

	if len(p.images) == 0 && len(p.files) == 0 {
		return fmt.Errorf("%s holds no content cargoship can use: no image in %s/%s carries a %s annotation, and %s declares no component files",
			p.source, imagesDir, ocispec.ImageIndexFile, ocispec.AnnotationRefName, zarfYAML)
	}
	return nil
}

// readDefinition parses the package's zarf.yaml into the version-independent model.
//
// A built zarf.yaml may hold one document per apiVersion, so the newest version the vendored
// zarf understands is the one read -- the same rule internal/cfg applies to a distro.yaml.
func (p *Package) readDefinition(ctx context.Context) (zarfapi.Package, error) {
	raw, err := os.ReadFile(filepath.Join(p.dir, zarfYAML))
	if err != nil {
		return zarfapi.Package{}, fmt.Errorf("%s is not a zarf package: %w", p.source, err)
	}
	parsed, err := parser.ParseBytes(raw, 0)
	if err != nil {
		return zarfapi.Package{}, fmt.Errorf("parsing %s of %s: %w", zarfYAML, p.source, err)
	}

	var (
		chosen   zarfapi.Package
		priority int
	)
	for i, doc := range parsed.Docs {
		if doc.Body == nil {
			continue
		}
		version, err := apiVersionFromNode(doc.Body)
		if err != nil {
			return zarfapi.Package{}, fmt.Errorf("document %d of %s: reading apiVersion: %w", i, zarfYAML, err)
		}
		pkg, docPriority, err := decodeDocument(doc.Body, version)
		if err != nil {
			return zarfapi.Package{}, fmt.Errorf("document %d of %s: %w", i, zarfYAML, err)
		}
		if docPriority == 0 {
			logger.From(ctx).Debug("skipping a zarf.yaml document in an unsupported apiVersion",
				"package", p.source, "apiVersion", version)
			continue
		}
		if docPriority > priority {
			chosen, priority = pkg, docPriority
		}
	}
	if priority == 0 {
		return zarfapi.Package{}, fmt.Errorf("%s declares no apiVersion the vendored zarf understands; it supports %v", zarfYAML, zarfapi.KnownAPIVersions())
	}
	return chosen, nil
}

// decodeDocument decodes one zarf.yaml document in the apiVersion it declares. A priority of 0
// means the apiVersion is not one this build understands.
func decodeDocument(node ast.Node, version string) (zarfapi.Package, int, error) {
	switch version {
	// An omitted apiVersion is zarf's legacy spelling of v1alpha1.
	case v1alpha1.APIVersion, "":
		var pkg v1alpha1.ZarfPackage
		if err := goyaml.NodeToValue(node, &pkg); err != nil {
			return zarfapi.Package{}, 0, err
		}
		return convert.PackageFromV1alpha1(pkg), 1, nil //nolint:staticcheck // SA1019: v1alpha1 is load-bearing here; see docs/dev/zarf-v1alpha1-surface.md
	case v1beta1.APIVersion:
		var pkg v1beta1.Package
		if err := goyaml.NodeToValue(node, &pkg); err != nil {
			return zarfapi.Package{}, 0, err
		}
		return convert.PackageFromV1beta1(pkg), 2, nil
	default:
		return zarfapi.Package{}, 0, nil
	}
}

func apiVersionFromNode(node ast.Node) (string, error) {
	var probe struct {
		APIVersion string `yaml:"apiVersion"`
	}
	if err := goyaml.NodeToValue(node, &probe); err != nil {
		return "", err
	}
	return probe.APIVersion, nil
}

// loadFiles indexes every component file by the source its definition declared.
func (p *Package) loadFiles(ctx context.Context, definition zarfapi.Package) {
	l := logger.From(ctx)
	p.files = map[string]file{}
	for _, component := range definition.Components {
		for idx, f := range component.Files {
			if f.Source == "" {
				continue
			}
			// zarf stores the extracted result of an extractPath entry, not the archive it
			// came from, while a distro definition's own ExtractPath would extract the
			// staged bytes a second time. Leaving these out of the index sends the file to
			// its declared source, which is the one place it is still an archive.
			if f.ExtractPath != "" {
				l.Debug("skipping a zarf package file stored pre-extracted",
					"package", p.source, "declared", f.Source, "extractPath", f.ExtractPath)
				continue
			}
			name := path.Base(filepath.ToSlash(f.Destination))
			if previous, duplicated := p.files[contentstore.CanonicalFileRef(f.Source)]; duplicated {
				l.Debug("zarf package declares one source in more than one component; using the last",
					"package", p.source, "declared", f.Source, "previous", previous.member)
			}
			p.files[contentstore.CanonicalFileRef(f.Source)] = file{
				// The path zarf gives a component file inside its tarball:
				// <component>/files/<index>/<basename of destination>.
				member:  path.Join(component.Name, filesDir, strconv.Itoa(idx), name),
				name:    name,
				tarball: filepath.Join(componentsDir, component.Name+".tar"),
			}
		}
	}
}

// loadImages opens the package's OCI layout and indexes it by original reference.
func (p *Package) loadImages(ctx context.Context) error {
	layoutDir := filepath.Join(p.dir, imagesDir)
	indexBytes, err := os.ReadFile(filepath.Join(layoutDir, ocispec.ImageIndexFile))
	if errors.Is(err, os.ErrNotExist) {
		// A package of files alone carries no images directory, which is not an error --
		// load rejects a package holding neither images nor files.
		p.images = map[string]ocispec.Descriptor{}
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading the image index of %s: %w", p.source, err)
	}
	var index ocispec.Index
	if err := json.Unmarshal(indexBytes, &index); err != nil {
		return fmt.Errorf("unmarshaling the image index of %s: %w", p.source, err)
	}

	// NewFromFS over contentstore.LayoutFS, rather than orasoci.New, so that a package missing
	// the oci-layout marker is read rather than written to. zarf writes its images through oras
	// and so normally has one, but a reader has no business creating it either way.
	p.store, err = orasoci.NewFromFS(ctx, contentstore.LayoutFS(os.DirFS(layoutDir)))
	if err != nil {
		return fmt.Errorf("opening the OCI layout of %s: %w", p.source, err)
	}

	p.images = map[string]ocispec.Descriptor{}
	for _, desc := range index.Manifests {
		// RefName is what zarf sets; BaseImageName is set to the same value beside it, and is
		// the one crane writes when an image arrives from a container daemon.
		ref := desc.Annotations[ocispec.AnnotationRefName]
		if ref == "" {
			ref = desc.Annotations[ocispec.AnnotationBaseImageName]
		}
		if ref == "" {
			continue
		}
		p.images[contentstore.CanonicalImageRef(ref)] = desc
	}
	return nil
}

// HasImage reports whether the package holds the image ref, spelled as a distro definition
// spells it.
func (p *Package) HasImage(ref string) bool {
	_, ok := p.images[contentstore.CanonicalImageRef(ref)]
	return ok
}

// File reports whether the package holds a component file declared as coming from req.Source --
// a URL or a path as it appeared in a distro definition -- and the name it is restored under.
//
// req.Arch is ignored, and the error is always nil. A zarf package's components[].files has no
// platform dimension: one entry is one file, and a package that carries two architectures of
// something carries them as two entries. Only a bundle that can hold several platforms under
// one reference -- a hauler store holding a vendor-published index -- has anything to answer
// here.
func (p *Package) File(req contentstore.FileRequest) (string, bool, error) {
	f, ok := p.files[contentstore.CanonicalFileRef(req.Source)]
	return f.name, ok, nil
}

// Source is the path the package was opened from, for messages and provenance.
func (p *Package) Source() string {
	return p.source
}

// Describe names the package in log lines.
func (p *Package) Describe() string {
	return "zarf package " + p.source
}

// Fetch writes the component file declared as coming from req.Source to dst, creating it.
func (p *Package) Fetch(ctx context.Context, req contentstore.FileRequest, dst string) (err error) {
	src := contentstore.CanonicalFileRef(req.Source)
	f, ok := p.files[src]
	if !ok {
		return fmt.Errorf("%s does not hold the file %s", p.source, src)
	}

	// One member out of one component tarball, into a directory of its own: the member path
	// carries the component name, so extracting it beside the package's own tree would collide
	// with the unpacked layout.
	staged, err := os.MkdirTemp("", "cargoship-zarf-file-")
	if err != nil {
		return fmt.Errorf("unable to create a staging directory for %s: %w", src, err)
	}
	defer func() {
		err = errors.Join(err, os.RemoveAll(staged))
	}()

	tarball := filepath.Join(p.dir, f.tarball)
	if err := archive.Decompress(ctx, tarball, staged, archive.DecompressOpts{Files: []string{f.member}}); err != nil {
		return fmt.Errorf("extracting %s from %s: %w", f.member, f.tarball, err)
	}
	// Decompress filters by exact member path and does not strip it, so the extracted file
	// keeps its component prefix.
	return copyFile(filepath.Join(staged, filepath.FromSlash(f.member)), dst)
}

func copyFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening %s: %w", src, err)
	}
	defer func() {
		err = errors.Join(err, in.Close())
	}()
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("creating %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		return errors.Join(fmt.Errorf("writing %s: %w", dst, err), out.Close())
	}
	return out.Close()
}

// Close removes anything Open unpacked. A package directory is left untouched.
func (p *Package) Close() error {
	if p.tempDir == "" {
		return nil
	}
	err := os.RemoveAll(p.tempDir)
	p.tempDir = ""
	return err
}

// Target presents the package's images as an oras target that resolves the references a distro
// definition uses.
//
// The layout's own tag index cannot be relied on: zarf tags each image under the reference it
// pulled it by, but a package of files alone has no layout at all, and nothing guarantees the
// tag survived a registry override. This maps the full reference back through the index
// annotation instead.
func (p *Package) Target() oras.ReadOnlyTarget {
	return &target{Package: p}
}

type target struct {
	*Package
}

// Fetch satisfies content.ReadOnlyStorage, shadowing Package.Fetch, whose signature names a file
// by its declared source and writes it to a path.
func (t *target) Fetch(ctx context.Context, desc ocispec.Descriptor) (io.ReadCloser, error) {
	if t.store == nil {
		return nil, fmt.Errorf("%s holds no images", t.source)
	}
	return t.store.Fetch(ctx, desc)
}

func (t *target) Exists(ctx context.Context, desc ocispec.Descriptor) (bool, error) {
	if t.store == nil {
		return false, nil
	}
	return t.store.Exists(ctx, desc)
}

func (t *target) Resolve(_ context.Context, reference string) (ocispec.Descriptor, error) {
	desc, ok := t.images[contentstore.CanonicalImageRef(reference)]
	if !ok {
		return ocispec.Descriptor{}, fmt.Errorf("%s does not hold the image %s", t.source, reference)
	}
	return desc, nil
}
