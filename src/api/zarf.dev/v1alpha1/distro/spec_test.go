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

package distro

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTarballSuffix(t *testing.T) {
	for _, tt := range []struct {
		name        string
		compression string
		want        string
	}{
		{name: "unset defaults to uncompressed", compression: "", want: ".tar"},
		{name: "none", compression: CompressionNone, want: ".tar"},
		{name: "gzip", compression: CompressionGzip, want: ".tar.gz"},
		{name: "zstd", compression: CompressionZstd, want: ".tar.zst"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ZarfDistroImageConfig{Compression: tt.compression}.TarballSuffix()
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestTarballSuffixUnsupported(t *testing.T) {
	_, err := ZarfDistroImageConfig{Compression: "bzip2"}.TarballSuffix()
	require.ErrorContains(t, err, `unsupported image compression "bzip2"`)
}
