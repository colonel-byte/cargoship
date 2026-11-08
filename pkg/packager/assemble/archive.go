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

package assemble

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/pkg/coci"
	"github.com/colonel-byte/cargoship/pkg/helpers"
	"github.com/colonel-byte/cargoship/pkg/utils"
	zlang "github.com/zarf-dev/zarf/src/config/lang"
	"github.com/zarf-dev/zarf/src/pkg/archive"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// archiveKey identifies the one archive a group of file entries all extract from.
//
// It is built from the entry as the definition wrote it rather than from the resolved source,
// because the resolution is a function of exactly these three fields: an override matches on
// the declared source, and a content store is searched by source, architecture and layer. Two
// entries agreeing here therefore resolve to the same bytes, which is what lets them share one
// extraction.
type archiveKey struct {
	source string
	layer  string
	arch   string
}

func keyFor(file v1alpha1.File) archiveKey {
	return archiveKey{source: file.Source, layer: file.Layer, arch: fileArch(file)}
}

// archiveExtractor decompresses an archive once per build, however many entries take files out
// of it.
//
// A distro definition commonly extracts a whole engine release one member at a time -- the
// binary, two scripts and two unit files out of one rke2 tarball, per architecture -- and each
// of those was a separate walk of the same 43 MiB gzip stream. The fetch was already shared,
// by the layer cache for an oci:// source and by utils.CachedArchive for a URL, so what was
// left was pure CPU: ~0.64s per entry, which on a ten-entry definition was most of the build.
//
// The extraction itself always supported this -- archive.DecompressOpts.Files is a set, and
// filterHandler pulls every match in one walk. It was the per-entry call structure that did
// not use it, so the fix is to collect what each archive is asked for before staging anything
// and then ask once.
type archiveExtractor struct {
	// wanted is every member each archive is asked for, collected before the first extraction.
	wanted map[archiveKey][]string
	// root is the directory extractions are written under, one subdirectory per archive.
	root string

	mu sync.Mutex
	// done is the extraction each archive produced, so a second member is read rather than
	// extracted again.
	done map[archiveKey]*extraction
	// digests memoises each archive's content digest, so resolving one costs one reference
	// resolution per archive rather than one per entry.
	digests map[archiveKey]string
}

// extraction is one archive's extracted members, and whatever went wrong producing them.
type extraction struct {
	dir string
	err error
}

// newArchiveExtractor collects the members each archive is asked for across every file list a
// build stages, so that an archive shared between spec.config.files and spec.config.os.files
// is still only walked once.
func newArchiveExtractor(root string, lists ...[]*v1alpha1.File) *archiveExtractor {
	e := &archiveExtractor{
		wanted:  map[archiveKey][]string{},
		root:    root,
		done:    map[archiveKey]*extraction{},
		digests: map[archiveKey]string{},
	}
	for _, list := range lists {
		for _, file := range list {
			if file == nil || file.ExtractPath == "" {
				continue
			}
			key := keyFor(*file)
			if !slices.Contains(e.wanted[key], file.ExtractPath) {
				e.wanted[key] = append(e.wanted[key], file.ExtractPath)
			}
		}
	}
	return e
}

// archiveSource is how one archive is obtained.
type archiveSource struct {
	// digest returns a verified content digest of the archive, or "" when the source has
	// none to give. It must not fetch the archive.
	digest func(ctx context.Context) (string, error)
	// stage writes the archive into the directory it is given and returns the path it wrote.
	stage func(ctx context.Context, dir string) (string, error)
}

// member returns the path to one extracted member, decompressing the archive on the first ask
// and serving every later one from what that produced.
//
// src.stage is called at most once per archive, and only when something is actually needed out
// of it that is not already cached. Passing it as a function keeps the three ways an archive
// arrives -- a registry layer, a cached download, a path beside the definition -- out of here,
// since none of them changes what extracting it means.
func (e *archiveExtractor) member(ctx context.Context, key archiveKey, extractPath string, src archiveSource) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// A member cached by an earlier build is served without touching the archive at all --
	// no fetch and, more to the point, no decompression. An archive whose every member is
	// cached is never staged.
	if cached, ok := utils.CachedMember(ctx, e.digestFor(ctx, key, src), extractPath); ok {
		return cached, nil
	}

	result, ok := e.done[key]
	if !ok {
		result = e.extract(ctx, key, src)
		e.done[key] = result
	}
	if result.err != nil {
		return "", result.err
	}

	member := filepath.Join(result.dir, filepath.FromSlash(extractPath))
	if digest := e.digestFor(ctx, key, src); digest != "" {
		utils.CacheMember(ctx, digest, extractPath, member)
	}
	return member, nil
}

// digestFor returns the archive's content digest, asking the source at most once per archive
// per build. An empty result means this archive has no digest to key a cache on, which is the
// case for every source that is not an OCI layer -- see utils.CachedArchive.
func (e *archiveExtractor) digestFor(ctx context.Context, key archiveKey, src archiveSource) string {
	if digest, ok := e.digests[key]; ok {
		return digest
	}
	var digest string
	if src.digest != nil {
		resolved, err := src.digest(ctx)
		if err != nil {
			// Not fatal: a digest is an optimisation, and whatever stopped this from
			// resolving will be reported properly by the fetch that follows.
			logger.From(ctx).Debug("unable to identify an archive for caching", "source", key.source, "error", err)
		}
		digest = resolved
	}
	e.digests[key] = digest
	return digest
}

// extract stages one archive and decompresses every member anything asked it for.
func (e *archiveExtractor) extract(ctx context.Context, key archiveKey, src archiveSource) *extraction {
	members := e.wanted[key]
	if len(members) == 0 {
		// Only reachable if a caller asks for a member of an archive newArchiveExtractor never
		// saw, which would mean the file list changed after the plan was built.
		return &extraction{err: fmt.Errorf("no members were planned for %s", key.source)}
	}

	dir := filepath.Join(e.root, strconv.Itoa(len(e.done)))
	if err := os.MkdirAll(dir, helpers.ReadExecuteAllWriteUser); err != nil {
		return &extraction{err: err}
	}

	// The stager picks the file name, not this: an extraction identifies a format from the
	// name rather than from the bytes, and only the stager knows what the archive is called
	// -- for an oci:// source that is the layer's title annotation, which nothing in the
	// entry carries when the manifest holds a single layer.
	staged := filepath.Join(dir, "archive")
	if err := os.MkdirAll(staged, helpers.ReadExecuteAllWriteUser); err != nil {
		return &extraction{err: err}
	}
	archivePath, err := src.stage(ctx, staged)
	if err != nil {
		return &extraction{err: err}
	}
	name := filepath.Base(archivePath)
	// The archive is not wanted once its members are out, and an engine tarball is tens of
	// mebibytes per architecture. A URL source is read where the shared cache holds it, so
	// only what was staged into this directory is removed.
	defer func() {
		if err := os.RemoveAll(staged); err != nil {
			logger.From(ctx).Warn("unable to remove a staged archive", "path", staged, "error", err)
		}
	}()

	logger.From(ctx).Debug("extracting archive", "archive", name, "members", len(members))
	if err := archive.Decompress(ctx, archivePath, dir, archive.DecompressOpts{Files: members}); err != nil {
		return &extraction{err: fmt.Errorf(zlang.ErrFileExtract, members, name, err)}
	}
	return &extraction{dir: dir}
}

// cleanup removes everything the extractor wrote.
func (e *archiveExtractor) cleanup() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var errs []error
	for _, result := range e.done {
		if result.dir != "" {
			errs = append(errs, os.RemoveAll(result.dir))
		}
	}
	e.done = map[archiveKey]*extraction{}
	return errors.Join(errs...)
}

// archiveName is what the staged archive is called, which decides how its format is
// identified. The declared source is used rather than the resolved one: a mirror may serve the
// same artifact under a different path, and an overridden URL that drops the extension would
// otherwise make an archive unidentifiable.
func archiveName(file v1alpha1.File) string {
	if file.Layer != "" {
		return filepath.Base(filepath.FromSlash(file.Layer))
	}
	if helpers.IsURL(file.Source) {
		if base, err := helpers.ExtractBasePathFromURL(file.Source); err == nil && base != "" {
			return base
		}
	}
	return filepath.Base(filepath.FromSlash(file.Source))
}

// localSource resolves a source that is a path rather than a URL. A resolved local override is
// already absolute, so it is never re-resolved against the distro definition directory.
func localSource(source, distroPath string) string {
	if filepath.IsAbs(source) {
		return source
	}
	return filepath.Join(distroPath, source)
}

// newArchiveSource describes how to obtain the archive an entry extracts from, by whichever of
// the three routes its source names.
//
// Only an OCI layer offers a digest. A URL-sourced archive carries none of its own -- the
// entry's shasum is the digest of the extracted member, not of the archive -- which is the
// same reason utils.CachedArchive has to key on the URL, and why a member taken out of one is
// not cached between builds.
func newArchiveSource(source string, file v1alpha1.File, distroPath string, ociOpts ociOptions) archiveSource {
	sel := coci.FileSelector{Arch: fileArch(file), Layer: file.Layer}
	switch {
	case helpers.IsOCIURL(source):
		return archiveSource{
			digest: func(ctx context.Context) (string, error) {
				return coci.LayerDigest(ctx, source, sel, ociOpts.FileOptions)
			},
			stage: func(ctx context.Context, dir string) (string, error) {
				// The layer itself, not a member of it: the extraction happens once, in the
				// extractor. coci names the file after the layer's title, which is what
				// makes the format identifiable.
				return coci.FetchLayerFile(ctx, source, sel, dir, ociOpts.FileOptions)
			},
		}
	case helpers.IsURL(source):
		return archiveSource{
			stage: func(ctx context.Context, _ string) (string, error) {
				// Read where the shared cache holds it rather than copied, since that is
				// already a stable path.
				cached, err := utils.CachedArchive(ctx, source, archiveName(file))
				if err != nil {
					return "", fmt.Errorf(zlang.ErrDownloading, source, err)
				}
				return cached, nil
			},
		}
	default:
		return archiveSource{
			stage: func(_ context.Context, _ string) (string, error) {
				return localSource(source, distroPath), nil
			},
		}
	}
}
