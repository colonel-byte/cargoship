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
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	orasoci "oras.land/oras-go/v2/content/oci"

	"github.com/colonel-byte/cargoship/pkg/contentstore"
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

// file is a file artifact held in the store. It is internal to the reader: a caller asks for a
// file by the source a distro definition declared and gets bytes, not a descriptor, because the
// other content stores hold files that have no descriptor to hand back.
type file struct {
	// descriptor is the content layer, not the manifest.
	descriptor ocispec.Descriptor
	// name is the layer's title annotation -- the filename hauler recorded.
	name string
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
	files   map[string]file
}

// Open reads the hauler store at path, which may be a store directory or an archive written by
// `hauler store save`. An archive is unpacked into a temporary directory owned by the returned
// Store.
func Open(ctx context.Context, path string) (*Store, error) {
	layoutDir, tempDir, err := contentstore.Unpack(ctx, path)
	if err != nil {
		return nil, err
	}
	s, err := OpenDir(ctx, layoutDir, path)
	if err != nil {
		return nil, errors.Join(err, os.RemoveAll(tempDir))
	}
	s.tempDir = tempDir
	return s, nil
}

// OpenDir reads an already-unpacked hauler store at dir, naming it in messages and recorded
// provenance as source. The caller keeps ownership of dir: the returned Store's Close does not
// remove it.
//
// This is the entry point for a caller that has unpacked a bundle itself in order to work out
// which format it is -- see openContentStore in pkg/distro.
func OpenDir(ctx context.Context, dir, source string) (*Store, error) {
	s := &Store{source: source}
	if err := s.load(ctx, dir); err != nil {
		return nil, err
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

	// NewFromFS over contentstore.LayoutFS, rather than orasoci.New: a live hauler store carries
	// no oci-layout marker (hauler only writes one into an archive), and New would create it --
	// writing to a store cargoship was asked only to read.
	s.store, err = orasoci.NewFromFS(ctx, contentstore.LayoutFS(os.DirFS(dir)))
	if err != nil {
		return fmt.Errorf("opening the OCI layout of %s: %w", s.source, err)
	}

	s.images = map[string]ocispec.Descriptor{}
	s.files = map[string]file{}
	for _, desc := range index.Manifests {
		ref := desc.Annotations[AnnotationOriginalRef]
		if ref == "" || isAttached(desc) {
			continue
		}
		entry, isFile, err := s.classify(ctx, desc)
		if err != nil {
			return err
		}
		if isFile {
			s.files[ref] = entry
			continue
		}
		s.images[contentstore.CanonicalImageRef(ref)] = desc
	}

	if len(s.images) == 0 && len(s.files) == 0 {
		return fmt.Errorf("%s holds no content cargoship can use: no entry carries a %s annotation, which is how hauler records what an artifact came from", s.source, AnnotationOriginalRef)
	}
	return nil
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
func (s *Store) classify(ctx context.Context, desc ocispec.Descriptor) (file, bool, error) {
	// An index is always a multi-architecture image; there is no file artifact shaped as one.
	if desc.MediaType == ocispec.MediaTypeImageIndex || desc.MediaType == mediaTypeDockerManifestList {
		return file{}, false, nil
	}

	manifestBytes, err := s.fetchAll(ctx, desc)
	if err != nil {
		return file{}, false, fmt.Errorf("reading manifest %s from %s: %w", desc.Digest, s.source, err)
	}
	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return file{}, false, fmt.Errorf("unmarshaling manifest %s from %s: %w", desc.Digest, s.source, err)
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
	return file{}, false, nil
}

// fileLayer picks the single content layer out of a file artifact's manifest.
func fileLayer(manifest ocispec.Manifest) (file, bool, error) {
	for _, layer := range manifest.Layers {
		if title := layer.Annotations[ocispec.AnnotationTitle]; title != "" {
			return file{descriptor: layer, name: title}, true, nil
		}
	}
	// A file artifact with no titled layer has no name to restore it under, so there is
	// nothing useful to return; treating it as an image would be worse.
	return file{}, false, errors.New("hauler file artifact carries no titled layer")
}

// mediaTypeDockerManifestList is the Docker spelling of an image index, which is what hauler
// records for a multi-architecture image pulled from a Docker registry.
const mediaTypeDockerManifestList = "application/vnd.docker.distribution.manifest.list.v2+json"

// HasImage reports whether the store holds the image ref, spelled as a distro definition
// spells it.
func (s *Store) HasImage(ref string) bool {
	_, ok := s.images[contentstore.CanonicalImageRef(ref)]
	return ok
}

// File reports whether the store holds the file it recorded as coming from src -- a URL or an
// absolute path as it appeared in a distro definition -- and the name hauler recorded it under.
func (s *Store) File(src string) (string, bool) {
	f, ok := s.files[src]
	return f.name, ok
}

// Source is the path the store was opened from, for messages and provenance.
func (s *Store) Source() string {
	return s.source
}

// Describe names the store in log lines, satisfying images.ImageSource.
func (s *Store) Describe() string {
	return "hauler store " + s.source
}

// Fetch writes the file the store recorded as coming from src to dst, creating it.
func (s *Store) Fetch(ctx context.Context, src, dst string) (err error) {
	f, ok := s.files[src]
	if !ok {
		return fmt.Errorf("%s does not hold the file %s", s.source, src)
	}
	rc, err := s.store.Fetch(ctx, f.descriptor)
	if err != nil {
		return fmt.Errorf("fetching %s from %s: %w", f.descriptor.Digest, s.source, err)
	}
	defer func() {
		err = errors.Join(err, rc.Close())
	}()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("creating %s: %w", dst, err)
	}
	if _, err := io.Copy(out, rc); err != nil {
		return errors.Join(fmt.Errorf("writing %s: %w", dst, err), out.Close())
	}
	return out.Close()
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
	desc, ok := t.images[contentstore.CanonicalImageRef(reference)]
	if !ok {
		return ocispec.Descriptor{}, fmt.Errorf("%s does not hold the image %s", t.source, reference)
	}
	return desc, nil
}
