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
	"slices"
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
const (
	// MediaTypeFileConfigLocal is what hauler records for a file added from a local path.
	MediaTypeFileConfigLocal = "application/vnd.content.hauler.file.local.config.v1+json"
	// MediaTypeFileConfigDirectory is what hauler records for a file added from a directory.
	MediaTypeFileConfigDirectory = "application/vnd.content.hauler.file.directory.config.v1+json"
	// MediaTypeFileConfigHTTP is what hauler records for a file added from a URL. It is also
	// what cargoship writes when it mirrors a package's files to a registry, so that what the
	// mirror holds is a shape this reader and hauler's own tooling both already understand.
	MediaTypeFileConfigHTTP = "application/vnd.content.hauler.file.http.config.v1+json"
)

// fileConfigMediaTypes is the set above, for classifying a manifest on the way in.
var fileConfigMediaTypes = map[string]bool{
	MediaTypeFileConfigLocal:     true,
	MediaTypeFileConfigDirectory: true,
	MediaTypeFileConfigHTTP:      true,
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

// fileKey identifies a file artifact in the store: the reference hauler recorded it under,
// plus the architecture of the platform child it was found in.
//
// A file artifact published as a single manifest is keyed with an empty arch and answers for
// every architecture, which is what a store holding one platform per reference has always
// produced. One published as an image index is keyed per child, because the reference alone
// no longer names bytes.
type fileKey struct {
	ref  string
	arch string
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
	files   map[fileKey]file
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
	s.files = map[fileKey]file{}
	for _, desc := range index.Manifests {
		ref := desc.Annotations[AnnotationOriginalRef]
		if ref == "" || isAttached(desc) {
			continue
		}
		if isIndex(desc) {
			held, err := s.classifyIndex(ctx, ref, desc)
			if err != nil {
				return err
			}
			if len(held) == 0 {
				s.images[contentstore.CanonicalImageRef(ref)] = desc
				continue
			}
			for arch, entry := range held {
				s.files[fileKey{ref: ref, arch: arch}] = entry
			}
			continue
		}
		entry, isFile, err := s.classify(ctx, desc)
		if err != nil {
			return err
		}
		if isFile {
			s.files[fileKey{ref: ref}] = entry
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

// isIndex reports whether desc describes an image index, in either spelling.
func isIndex(desc ocispec.Descriptor) bool {
	return desc.MediaType == ocispec.MediaTypeImageIndex || desc.MediaType == mediaTypeDockerManifestList
}

// classifyIndex classifies the children of an index entry, returning the file artifact each
// platform child holds, keyed by architecture. An empty result means the index is an image,
// which is what a multi-architecture image pulled into a store looks like.
//
// An index was assumed to be an image for as long as this reader existed, on the premise that
// no file artifact is shaped as one. At least one vendor publishes an air-gapped engine bundle
// exactly that way -- one tag, an index, and one titled layer per platform -- so the premise
// was wrong and the store it was given resolved nothing. See issue #1033.
func (s *Store) classifyIndex(ctx context.Context, ref string, desc ocispec.Descriptor) (map[string]file, error) {
	indexBytes, err := s.fetchAll(ctx, desc)
	if err != nil {
		return nil, fmt.Errorf("reading index %s from %s: %w", desc.Digest, s.source, err)
	}
	var index ocispec.Index
	if err := json.Unmarshal(indexBytes, &index); err != nil {
		return nil, fmt.Errorf("unmarshaling index %s from %s: %w", desc.Digest, s.source, err)
	}

	held := map[string]file{}
	for _, child := range index.Manifests {
		// A nested index is not something either format produces, and descending it would
		// have no architecture to key the result on.
		if isIndex(child) {
			continue
		}
		entry, isFile, err := s.classify(ctx, child)
		if err != nil {
			return nil, err
		}
		if !isFile {
			continue
		}
		arch := ""
		if child.Platform != nil {
			arch = child.Platform.Architecture
		}
		if _, clash := held[arch]; clash {
			return nil, fmt.Errorf("%s holds %s as an index with two %s children, so the reference does not name one file", s.source, ref, archName(arch))
		}
		held[arch] = entry
	}

	// A mixed index -- some children files, some not -- has no reading that is better than a
	// guess, and guessing here would silently ship the wrong bytes.
	if len(held) > 0 && len(held) != len(index.Manifests) {
		return nil, fmt.Errorf("%s holds %s as an index of %d manifests, only %d of which are file artifacts", s.source, ref, len(index.Manifests), len(held))
	}
	return held, nil
}

// archName spells an empty architecture for a message, where "" would read as a gap in the
// sentence rather than as the absence of a platform.
func archName(arch string) string {
	if arch == "" {
		return "unplatformed"
	}
	return arch
}

// HasImage reports whether the store holds the image ref, spelled as a distro definition
// spells it.
func (s *Store) HasImage(ref string) bool {
	_, ok := s.images[contentstore.CanonicalImageRef(ref)]
	return ok
}

// File reports whether the store holds the file it recorded as coming from req.Source -- a URL
// or an absolute path as it appeared in a distro definition -- and the name hauler recorded it
// under.
//
// It returns an error, rather than reporting the file as absent, when the store holds the
// reference for several architectures and the request names none. That is not a miss: the
// bytes are there, and more than one answer is. Falling back to the declared source would
// reach the network for content the operator staged, and picking a platform would ship a
// binary the node cannot run.
func (s *Store) File(req contentstore.FileRequest) (string, bool, error) {
	f, ok, err := s.file(req)
	return f.name, ok, err
}

// file resolves a request against the index built at load time.
func (s *Store) file(req contentstore.FileRequest) (file, bool, error) {
	if f, ok := s.files[fileKey{ref: req.Source, arch: req.Arch}]; ok {
		return f, true, nil
	}
	// A file artifact published as a single manifest carries no platform and answers for
	// every architecture, including a request that names one.
	if f, ok := s.files[fileKey{ref: req.Source}]; ok {
		return f, true, nil
	}
	if req.Arch == "" {
		if arches := s.fileArches(req.Source); len(arches) > 0 {
			return file{}, false, fmt.Errorf("%s holds %s for %s, and the file entry selects no single architecture: name one in its selector.arch", s.source, req.Source, strings.Join(arches, ", "))
		}
	}
	// A store that holds the reference for other architectures but not for this one is a
	// partial store, which is a miss and not an error.
	return file{}, false, nil
}

// fileArches lists the architectures the store holds ref for, sorted, for a message.
func (s *Store) fileArches(ref string) []string {
	var arches []string
	for key := range s.files {
		if key.ref == ref {
			arches = append(arches, archName(key.arch))
		}
	}
	slices.Sort(arches)
	return arches
}

// Source is the path the store was opened from, for messages and provenance.
func (s *Store) Source() string {
	return s.source
}

// Describe names the store in log lines, satisfying images.ImageSource.
func (s *Store) Describe() string {
	return "hauler store " + s.source
}

// Fetch writes the file the store recorded as coming from req.Source to dst, creating it.
func (s *Store) Fetch(ctx context.Context, req contentstore.FileRequest, dst string) (err error) {
	f, ok, err := s.file(req)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s does not hold the file %s", s.source, req.Source)
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
