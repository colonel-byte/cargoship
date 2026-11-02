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

// Package haulerstore reads a hauler store (https://github.com/hauler-dev/hauler) as a source
// of images and files for a package build.
//
// A store is a plain OCI image layout, and every entry in its index carries a
// "hauler.dev/original-ref" annotation holding the content's original identity: a
// registry/repo:tag for an image, and the source URL for a file. Those are the same strings a
// distro definition writes in imageConfig.images and File.Source, so resolving a definition
// against a haul is a map lookup rather than a heuristic.
package haulerstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/pkg/archive"
	"oras.land/oras-go/v2"
	orasoci "oras.land/oras-go/v2/content/oci"
)

// Hauler's index annotations. OriginalRef is the only one cargoship matches on; the other two
// are read to exclude entries that are attached to content rather than being content.
const (
	// AnnotationOriginalRef holds a registry/repo:tag for an image and the source URL for a
	// file, as hauler recorded it at `hauler store add` time.
	AnnotationOriginalRef = "hauler.dev/original-ref"
	// AnnotationSubjectDigest is set on an entry that describes another entry -- a signature,
	// an attestation, an SBOM.
	AnnotationSubjectDigest = "hauler.dev/subject-digest"
	// AnnotationKind is hauler's own content classification. It is not a reliable way to tell
	// a file from an image (see classify), but it does reliably mark the attached kinds.
	AnnotationKind = "kind"
)

// The hauler kinds that describe other content rather than being content themselves. A cosign
// signature carries the same OriginalRef as the image it signs, and -- because its config is a
// real OCI image config -- is otherwise indistinguishable from that image. Excluding these
// kinds is what stops a signature from being indexed over the image it belongs to.
var attachedKinds = map[string]bool{
	"dev.hauler/sigs":  true,
	"dev.hauler/atts":  true,
	"dev.hauler/sboms": true,
}

const attachedKindPrefix = "dev.hauler/referrers/"

// The config media types hauler gives a file artifact, one per source it was added from.
var fileConfigMediaTypes = map[string]bool{
	"application/vnd.content.hauler.file.local.config.v1+json":     true,
	"application/vnd.content.hauler.file.directory.config.v1+json": true,
	"application/vnd.content.hauler.file.http.config.v1+json":      true,
}

// chunkSuffix matches the trailing part number hauler gives the pieces of a chunked haul.
var chunkSuffix = regexp.MustCompile(`\.\d{3}$`)

// File is a file artifact held in the store.
type File struct {
	// Descriptor is the content layer, not the manifest.
	Descriptor ocispec.Descriptor
	// Name is the layer's title annotation -- the filename hauler recorded.
	Name string
}

// Store is an opened hauler store. Close removes anything Open unpacked, so every Store must
// be closed.
type Store struct {
	// source is the path the caller passed, used in messages and recorded provenance.
	source string
	// tempDir is the directory an archive was unpacked into, and is empty for a store
	// directory, which is never written to or removed.
	tempDir string
	store   *orasoci.ReadOnlyStore
	images  map[string]ocispec.Descriptor
	files   map[string]File
}

// Open reads the hauler store at path, which may be a store directory or an archive written by
// `hauler store save`. An archive is unpacked into a temporary directory owned by the returned
// Store.
func Open(ctx context.Context, path string) (*Store, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("unable to read hauler store %q: %w", path, err)
	}

	s := &Store{source: path}
	layoutDir := path
	if !info.IsDir() {
		// A chunked haul is only meaningful as a set, and reassembling one is hauler's job.
		// Saying so beats failing inside the decompressor on a truncated stream.
		if chunkSuffix.MatchString(path) {
			return nil, fmt.Errorf("%s looks like one chunk of a split hauler archive; rejoin it with `hauler store load` and pass the resulting store", path)
		}
		layoutDir, err = os.MkdirTemp("", "cargoship-haul-")
		if err != nil {
			return nil, fmt.Errorf("unable to create a temporary directory for %s: %w", path, err)
		}
		s.tempDir = layoutDir
		if err := archive.Decompress(ctx, path, layoutDir, archive.DecompressOpts{}); err != nil {
			return nil, errors.Join(fmt.Errorf("failed to decompress hauler archive %s: %w", path, err), s.Close())
		}
	}

	if err := s.load(ctx, layoutDir); err != nil {
		return nil, errors.Join(err, s.Close())
	}
	return s, nil
}

// load opens the OCI layout at dir and indexes it by original reference.
func (s *Store) load(ctx context.Context, dir string) error {
	indexBytes, err := os.ReadFile(filepath.Join(dir, ocispec.ImageIndexFile))
	if err != nil {
		return fmt.Errorf("%s is not a hauler store: %w", s.source, err)
	}
	var index ocispec.Index
	if err := json.Unmarshal(indexBytes, &index); err != nil {
		return fmt.Errorf("unmarshaling the index of %s: %w", s.source, err)
	}

	// NewFromFS over a layoutFS, rather than orasoci.New: a live hauler store carries no oci-layout marker
	// (hauler only writes one into an archive), and New would create it -- writing to a store
	// cargoship was asked only to read.
	s.store, err = orasoci.NewFromFS(ctx, layoutFS{os.DirFS(dir)})
	if err != nil {
		return fmt.Errorf("opening the OCI layout of %s: %w", s.source, err)
	}

	s.images = map[string]ocispec.Descriptor{}
	s.files = map[string]File{}
	for _, desc := range index.Manifests {
		ref := desc.Annotations[AnnotationOriginalRef]
		if ref == "" || isAttached(desc) {
			continue
		}
		file, isFile, err := s.classify(ctx, desc)
		if err != nil {
			return err
		}
		if isFile {
			s.files[ref] = file
			continue
		}
		s.images[canonicalImageRef(ref)] = desc
	}

	if len(s.images) == 0 && len(s.files) == 0 {
		return fmt.Errorf("%s holds no content cargoship can use: no entry carries a %s annotation, which is how hauler records what an artifact came from", s.source, AnnotationOriginalRef)
	}
	return nil
}

// canonicalImageRef puts an image reference into the one spelling two equal references share,
// so the store is indexed and searched by the same key.
//
// Hauler records what it collected under the registry's canonical host: an image added as
// docker.io/rancher/pause:3.10 comes back as index.docker.io/rancher/pause:3.10, which is the
// same image under a name a distro definition does not use. The same split applies to an
// official image, where docker.io/nginx, nginx and docker.io/library/nginx are one reference
// written three ways. A reference that will not parse is keyed as it was written -- it would
// not have matched anything anyway, and failing the lookup beats failing the open.
func canonicalImageRef(ref string) string {
	parsed, err := name.ParseReference(ref)
	if err != nil {
		return ref
	}
	return parsed.Name()
}

// isAttached reports whether desc describes other content -- a signature, attestation, SBOM or
// referrer -- rather than being content in its own right.
func isAttached(desc ocispec.Descriptor) bool {
	if desc.Annotations[AnnotationSubjectDigest] != "" {
		return true
	}
	kind := desc.Annotations[AnnotationKind]
	return attachedKinds[kind] || strings.HasPrefix(kind, attachedKindPrefix)
}

// classify decides whether an index entry is a file artifact or an image.
//
// Hauler's own "kind" annotation cannot answer this: it reads dev.hauler/image on file
// artifacts too. The manifest's config media type can, with one carve-out hauler itself
// implements -- some artifacts are stored under an OCI image config but hold file content,
// which a layer title gives away.
func (s *Store) classify(ctx context.Context, desc ocispec.Descriptor) (File, bool, error) {
	// An index is always a multi-architecture image; there is no file artifact shaped as one.
	if desc.MediaType == ocispec.MediaTypeImageIndex || desc.MediaType == mediaTypeDockerManifestList {
		return File{}, false, nil
	}

	manifestBytes, err := s.fetchAll(ctx, desc)
	if err != nil {
		return File{}, false, fmt.Errorf("reading manifest %s from %s: %w", desc.Digest, s.source, err)
	}
	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return File{}, false, fmt.Errorf("unmarshaling manifest %s from %s: %w", desc.Digest, s.source, err)
	}

	if fileConfigMediaTypes[manifest.Config.MediaType] {
		return fileLayer(manifest)
	}

	// The carve-out: an image config whose layers carry titles is file content stored as an
	// image, which is how hauler holds artifacts such as the RKE2 release binaries.
	for _, layer := range manifest.Layers {
		if layer.Annotations[ocispec.AnnotationTitle] != "" {
			return fileLayer(manifest)
		}
	}
	return File{}, false, nil
}

// fileLayer picks the single content layer out of a file artifact's manifest.
func fileLayer(manifest ocispec.Manifest) (File, bool, error) {
	for _, layer := range manifest.Layers {
		if title := layer.Annotations[ocispec.AnnotationTitle]; title != "" {
			return File{Descriptor: layer, Name: title}, true, nil
		}
	}
	// A file artifact with no titled layer has no name to restore it under, so there is
	// nothing useful to return; treating it as an image would be worse.
	return File{}, false, errors.New("hauler file artifact carries no titled layer")
}

// mediaTypeDockerManifestList is the Docker spelling of an image index, which is what hauler
// records for a multi-architecture image pulled from a Docker registry.
const mediaTypeDockerManifestList = "application/vnd.docker.distribution.manifest.list.v2+json"

// HasImage reports whether the store holds the image ref, spelled as a distro definition
// spells it.
func (s *Store) HasImage(ref string) bool {
	_, ok := s.images[canonicalImageRef(ref)]
	return ok
}

// File returns the file artifact the store recorded as coming from src, which is a URL or an
// absolute path as it appeared in a distro definition.
func (s *Store) File(src string) (File, bool) {
	f, ok := s.files[src]
	return f, ok
}

// Source is the path the store was opened from, for messages and provenance.
func (s *Store) Source() string {
	return s.source
}

// Describe names the store in log lines, satisfying images.ImageSource.
func (s *Store) Describe() string {
	return "hauler store " + s.source
}

// Fetch writes the content of desc to dst, creating it.
func (s *Store) Fetch(ctx context.Context, desc ocispec.Descriptor, dst string) (err error) {
	rc, err := s.store.Fetch(ctx, desc)
	if err != nil {
		return fmt.Errorf("fetching %s from %s: %w", desc.Digest, s.source, err)
	}
	defer func() {
		err = errors.Join(err, rc.Close())
	}()

	f, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("creating %s: %w", dst, err)
	}
	if _, err := io.Copy(f, rc); err != nil {
		return errors.Join(fmt.Errorf("writing %s: %w", dst, err), f.Close())
	}
	return f.Close()
}

// fetchAll reads a descriptor's bytes into memory. Only manifests are read this way.
func (s *Store) fetchAll(ctx context.Context, desc ocispec.Descriptor) (_ []byte, err error) {
	rc, err := s.store.Fetch(ctx, desc)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, rc.Close())
	}()
	return io.ReadAll(rc)
}

// Close removes anything Open unpacked. A store directory is left untouched.
func (s *Store) Close() error {
	if s.tempDir == "" {
		return nil
	}
	err := os.RemoveAll(s.tempDir)
	s.tempDir = ""
	return err
}

// Target presents the store as an oras target that resolves the references a distro definition
// uses.
//
// The store's own tag index cannot: hauler strips the registry host from
// org.opencontainers.image.ref.name, so "registry.k8s.io/pause:3.10" is tagged there as
// "pause:3.10" and would not resolve. This maps the full reference back through
// hauler.dev/original-ref.
func (s *Store) Target() oras.ReadOnlyTarget {
	return &target{Store: s}
}

type target struct {
	*Store
}

// Fetch satisfies content.ReadOnlyStorage, shadowing Store.Fetch, whose signature writes to a
// path rather than returning a reader.
func (t *target) Fetch(ctx context.Context, desc ocispec.Descriptor) (io.ReadCloser, error) {
	return t.store.Fetch(ctx, desc)
}

func (t *target) Exists(ctx context.Context, desc ocispec.Descriptor) (bool, error) {
	return t.store.Exists(ctx, desc)
}

func (t *target) Resolve(_ context.Context, reference string) (ocispec.Descriptor, error) {
	desc, ok := t.images[canonicalImageRef(reference)]
	if !ok {
		return ocispec.Descriptor{}, fmt.Errorf("%s does not hold the image %s", t.source, reference)
	}
	return desc, nil
}
