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

package haulerstore

import (
	"bytes"
	"io/fs"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// layoutMarker is the oci-layout file every OCI image layout is required to carry.
var layoutMarker = []byte(`{"imageLayoutVersion":"` + ocispec.ImageLayoutVersion + `"}`)

// layoutFS presents a directory as an OCI image layout, serving the oci-layout marker from
// memory when the directory has none.
//
// A live hauler store is a complete layout except for that marker -- hauler writes one only
// when saving an archive. The reader needs it (oras refuses a layout without one) and must not
// create it, because cargoship was asked to read the operator's store, not to modify it. So it
// is supplied here instead, for the lifetime of the read.
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
