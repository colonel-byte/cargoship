// Copyright 2026 colonel-byte
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package contentstore holds the pieces shared by the readers of a local content bundle --
// a hauler store, a zarf package -- so that two readers of the same OCI layout cannot drift
// apart in how they open it or how they key an image reference.
package contentstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/pkg/archive"
)

// layoutMarker is the oci-layout file every OCI image layout is required to carry.
var layoutMarker = []byte(`{"imageLayoutVersion":"` + ocispec.ImageLayoutVersion + `"}`)

// LayoutFS presents a directory as an OCI image layout, serving the oci-layout marker from
// memory when the directory has none.
//
// A live hauler store is a complete layout except for that marker -- hauler writes one only
// when saving an archive. The reader needs it (oras refuses a layout without one) and must not
// create it, because cargoship was asked to read the operator's store, not to modify it. So it
// is supplied here instead, for the lifetime of the read.
//
// A zarf package's images/ directory normally does carry the marker, because zarf writes that
// layout through oras, which creates one. The fallback below defers to a real file whenever
// there is one, so wrapping both readers in this costs nothing and leaves neither depending on
// a marker it did not write.
func LayoutFS(fsys fs.FS) fs.FS {
	return layoutFS{fsys}
}

type layoutFS struct {
	fs.FS
}

func (l layoutFS) Open(name string) (fs.File, error) {
	if name != ocispec.ImageLayoutFile {
		return l.FS.Open(name)
	}
	f, err := l.FS.Open(name)
	if err == nil {
		return f, nil
	}
	return &memFile{Reader: bytes.NewReader(layoutMarker), name: name, size: int64(len(layoutMarker))}, nil
}

// memFile is a read-only fs.File backed by a byte slice.
type memFile struct {
	*bytes.Reader
	name string
	size int64
}

func (f *memFile) Stat() (fs.FileInfo, error) { return memFileInfo{f}, nil }
func (f *memFile) Close() error               { return nil }

type memFileInfo struct{ f *memFile }

func (i memFileInfo) Name() string       { return i.f.name }
func (i memFileInfo) Size() int64        { return i.f.size }
func (i memFileInfo) Mode() fs.FileMode  { return 0o444 }
func (i memFileInfo) ModTime() time.Time { return time.Time{} }
func (i memFileInfo) IsDir() bool        { return false }
func (i memFileInfo) Sys() any           { return nil }

// CanonicalImageRef puts an image reference into the one spelling two equal references share,
// so that a bundle is indexed and searched by the same key.
//
// Both bundle formats record what they collected under the registry's canonical host: an image
// added as docker.io/rancher/pause:3.10 comes back as index.docker.io/rancher/pause:3.10, which
// is the same image under a name a distro definition does not use. The same split applies to an
// official image, where docker.io/nginx, nginx and docker.io/library/nginx are one reference
// written three ways. A reference that will not parse is keyed as it was written -- it would not
// have matched anything anyway, and failing the lookup beats failing the open.
func CanonicalImageRef(ref string) string {
	parsed, err := name.ParseReference(ref)
	if err != nil {
		return ref
	}
	return parsed.Name()
}

// Unpack makes the content bundle at path readable as a directory, returning the directory to
// read and the temporary directory the caller must remove when it is done -- which is empty
// when path was already a directory, because an operator's own store is never written to or
// removed.
//
// It is the one place a bundle is decompressed, so that deciding which format a bundle is can
// be a stat against the unpacked tree rather than a second pass over the archive.
func Unpack(ctx context.Context, path string) (dir string, tempDir string, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", "", fmt.Errorf("unable to read content store %q: %w", path, err)
	}
	if info.IsDir() {
		return path, "", nil
	}
	// A split archive is only meaningful as a set, and rejoining one is the producing tool's
	// job. Saying so beats failing inside the decompressor on a truncated stream, and the
	// suffix says which tool wrote it, so the message can name the command that undoes it.
	switch {
	case zarfPartSuffix.MatchString(path):
		return "", "", fmt.Errorf("%s looks like one part of a split zarf package; rejoin the parts and pass the resulting archive", path)
	case haulerChunkSuffix.MatchString(path):
		return "", "", fmt.Errorf("%s looks like one chunk of a split hauler archive; rejoin it with `hauler store load` and pass the resulting store", path)
	}

	tempDir, err = os.MkdirTemp("", "cargoship-content-")
	if err != nil {
		return "", "", fmt.Errorf("unable to create a temporary directory for %s: %w", path, err)
	}
	if err := archive.Decompress(ctx, path, tempDir, archive.DecompressOpts{}); err != nil {
		return "", "", errors.Join(fmt.Errorf("failed to decompress %s: %w", path, err), os.RemoveAll(tempDir))
	}
	return tempDir, tempDir, nil
}

// The trailing part numbers the two tools give the pieces of a split archive: hauler writes
// haul.tar.zst.001, zarf writes zarf-package-foo-amd64.tar.zst.part000. zarf is checked first,
// because its suffix also matches hauler's pattern.
var (
	zarfPartSuffix    = regexp.MustCompile(`\.part\d{3}$`)
	haulerChunkSuffix = regexp.MustCompile(`\.\d{3}$`)
)

// fileRefTagBytes is how much of the source digest goes into a file artifact's tag. Twelve hex
// characters is 48 bits, which makes a collision between two sources in one package not worth
// reasoning about, and still leaves a tag somebody can read back off a registry listing.
const fileRefTagBytes = 6

// invalidRepoChars matches every run of characters an OCI repository path component may not hold.
var invalidRepoChars = regexp.MustCompile(`[^a-z0-9]+`)

// FileRef is where the file declared as coming from source lives in a mirror under target.
//
// It is the one rule a writer and a reader of a mirrored file both apply, which is what lets a
// reader recompute a reference instead of discovering it -- a registry offers a repository
// catalogue and per-repository tags, never one annotated index the way a hauler store or a zarf
// package does, so there is nothing to walk and nothing to match against.
//
// The name carries the file's basename so a repository listing is legible to a person, and the
// tag is a digest of the whole source so it is correct. Keying on the basename alone would be
// wrong rather than merely ugly: every rke2 and etcd release archive is named the same across
// versions, so one version's file would be served for another's.
func FileRef(target, source string) (string, error) {
	if target == "" {
		return "", errors.New("target registry is required")
	}
	if source == "" {
		return "", errors.New("a file with no source has no reference to mirror it under")
	}

	// path.Base rather than filepath.Base: a source is a URL far more often than a path, and a
	// URL's separator is "/" whatever the management node's separator happens to be.
	base := path.Base(source)
	if i := strings.IndexAny(base, "?#"); i >= 0 {
		base = base[:i]
	}
	slug := strings.Trim(invalidRepoChars.ReplaceAllString(strings.ToLower(base), "-"), "-")
	if slug == "" {
		// A source whose last path segment slugs away to nothing -- "https://example.com/%20"
		// -- still needs somewhere to go, and the tag already distinguishes it.
		slug = "file"
	}

	sum := sha256.Sum256([]byte(source))
	return fmt.Sprintf("%s/files/%s:%s", strings.TrimSuffix(target, "/"), slug, hex.EncodeToString(sum[:fileRefTagBytes])), nil
}
