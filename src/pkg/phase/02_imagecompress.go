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
	"fmt"
	"io"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zstd"
)

// imageCompressWriter wraps w with the compressor for the given format. Closing
// the returned writer flushes the compressed stream but leaves w open, so the
// caller keeps owning w.
func imageCompressWriter(format string, w io.Writer) (io.WriteCloser, error) {
	switch format {
	case "", distro.CompressionNone:
		return nopWriteCloser{w}, nil
	case distro.CompressionGzip:
		return gzip.NewWriter(w), nil
	case distro.CompressionZstd:
		enc, err := zstd.NewWriter(w)
		if err != nil {
			return nil, fmt.Errorf("failed to create zstd writer: %w", err)
		}
		return enc, nil
	default:
		_, err := distro.ZarfDistroImageConfig{Compression: format}.TarballSuffix()
		return nil, err
	}
}

// nopWriteCloser turns a writer into a WriteCloser whose Close is a no-op, so
// the uncompressed path uses the same shape as the compressed ones.
type nopWriteCloser struct {
	io.Writer
}

// Close does nothing and always succeeds.
func (nopWriteCloser) Close() error { return nil }
