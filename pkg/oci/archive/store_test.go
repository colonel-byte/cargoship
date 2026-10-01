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

package archive

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	oci "oras.land/oras-go/v2/content/oci"
)

func TestOciArchiveStoreInfo(t *testing.T) {
	root := t.TempDir()
	src, err := oci.New(root)
	require.NoError(t, err)

	content := []byte("blob-bytes")
	desc := ocispec.Descriptor{
		MediaType: "application/octet-stream",
		Digest:    digest.FromBytes(content),
		Size:      int64(len(content)),
	}
	require.NoError(t, src.Push(context.Background(), desc, bytes.NewReader(content)))

	s := &OciArchiveStore{Root: root, Src: src}

	t.Run("resolves info for a known digest", func(t *testing.T) {
		info, err := s.Info(context.Background(), desc.Digest)
		require.NoError(t, err)
		require.Equal(t, desc.Digest, info.Digest)
		require.Equal(t, desc.Size, info.Size)
	})

	t.Run("errors for an unknown digest", func(t *testing.T) {
		_, err := s.Info(context.Background(), digest.FromString("unknown"))
		require.Error(t, err)
	})
}

func TestOciArchiveStoreReaderAt(t *testing.T) {
	root := t.TempDir()
	content := []byte("layer-bytes")
	dgst := digest.FromBytes(content)

	blobDir := filepath.Join(root, ocispec.ImageBlobsDir, dgst.Algorithm().String())
	require.NoError(t, os.MkdirAll(blobDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(blobDir, dgst.Encoded()), content, 0o600))

	s := &OciArchiveStore{Root: root}
	desc := ocispec.Descriptor{Digest: dgst, Size: int64(len(content))}

	t.Run("reads blob content from disk", func(t *testing.T) {
		ra, err := s.ReaderAt(context.Background(), desc)
		require.NoError(t, err)
		defer func() { require.NoError(t, ra.Close()) }()

		require.EqualValues(t, len(content), ra.Size())

		got := make([]byte, len(content))
		n, err := ra.ReadAt(got, 0)
		require.NoError(t, err)
		require.Equal(t, len(content), n)
		require.Equal(t, content, got)
	})

	t.Run("errors when the blob file does not exist", func(t *testing.T) {
		_, err := s.ReaderAt(context.Background(), ocispec.Descriptor{Digest: digest.FromString("missing")})
		require.Error(t, err)
	})
}
