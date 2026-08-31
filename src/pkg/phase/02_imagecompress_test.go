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

package phase

import (
	"bytes"
	"io"
	"testing"

	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

func TestImageCompressWriterUnknownFormat(t *testing.T) {
	_, err := imageCompressWriter("bzip2", &bytes.Buffer{})
	require.ErrorContains(t, err, `unsupported image compression "bzip2"`)
}

func TestImageCompressWriterRoundTrip(t *testing.T) {
	payload := bytes.Repeat([]byte("cargoship image layer\n"), 512)

	for _, tt := range []struct {
		name       string
		format     string
		decompress func(t *testing.T, r io.Reader) []byte
	}{
		{
			name:   "none passes bytes through",
			format: "none",
			decompress: func(t *testing.T, r io.Reader) []byte {
				b, err := io.ReadAll(r)
				require.NoError(t, err)
				return b
			},
		},
		{
			name:   "gzip",
			format: "gz",
			decompress: func(t *testing.T, r io.Reader) []byte {
				gr, err := gzip.NewReader(r)
				require.NoError(t, err)
				defer func() { require.NoError(t, gr.Close()) }()
				b, err := io.ReadAll(gr)
				require.NoError(t, err)
				return b
			},
		},
		{
			name:   "zstd",
			format: "zstd",
			decompress: func(t *testing.T, r io.Reader) []byte {
				zr, err := zstd.NewReader(r)
				require.NoError(t, err)
				defer zr.Close()
				b, err := io.ReadAll(zr)
				require.NoError(t, err)
				return b
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			w, err := imageCompressWriter(tt.format, &buf)
			require.NoError(t, err)

			_, err = w.Write(payload)
			require.NoError(t, err)
			require.NoError(t, w.Close())

			require.Equal(t, payload, tt.decompress(t, bytes.NewReader(buf.Bytes())))
			if tt.format != "none" {
				require.Less(t, buf.Len(), len(payload), "compressed output should be smaller than the input")
			}
		})
	}
}
