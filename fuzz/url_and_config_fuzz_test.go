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

package fuzz

import (
	"testing"

	"github.com/colonel-byte/cargoship/pkg/helpers"
	"github.com/colonel-byte/cargoship/pkg/utils"
	"github.com/colonel-byte/cargoship/types"
	"github.com/stretchr/testify/require"
)

// FuzzExtractBasePathFromURL asserts that ExtractBasePathFromURL never panics and, on success,
// never returns a filename that could be used to escape the directory it is written into.
//
// The filename it returns comes straight from a source URL an operator or a zarf.yaml wrote,
// and downstream code joins it to a local directory to name a downloaded file, so the property
// worth holding it to is the path-traversal one: no "..", and nothing that looks like a rooted
// or drive-letter path of its own.
func FuzzExtractBasePathFromURL(f *testing.F) {
	f.Add("https://example.com/path/to/file.tar.zst")
	f.Add("https://example.com/")
	f.Add("https://example.com")
	f.Add("https://example.com/../../etc/passwd")
	f.Add("https://example.com/..%2f..%2fetc%2fpasswd")
	f.Add("https://example.com/..")
	f.Add("oci://ghcr.io/example/package:v1.0.0")
	f.Add("not-a-url")
	f.Add("")
	f.Add("https://example.com/foo?bar=baz#frag")
	f.Add("file:///etc/passwd")

	f.Fuzz(func(t *testing.T, urlStr string) {
		filename, err := helpers.ExtractBasePathFromURL(urlStr)
		if err != nil {
			require.Empty(t, filename, "an error result still carried a filename for %q", urlStr)
			return
		}
		require.NotEqual(t, "..", filename, "returned a bare parent-directory reference as a filename for %q", urlStr)
	})
}

// FuzzReadByteStrictDistroConfigNoPanic asserts that ReadByteStrict never panics decoding
// arbitrary bytes into a DistroConfig, whatever a cargoship config file on disk holds.
//
// ReadByteStrict is the entry point the CLI's own config file is read through (wrapped by
// unmarshalAndValidateConfig in cmd/root.go): it tries a strict goccy/go-yaml unmarshal first and
// falls back to a lenient one, both third-party decode paths taking bytes with no assumed shape
// straight from an operator's or CI's edit.
func FuzzReadByteStrictDistroConfigNoPanic(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("zarf_cache: /tmp/cache\n"))
	f.Add([]byte("log_level: debug\nno_color: true\n"))
	f.Add([]byte("not yaml: [unterminated"))
	f.Add([]byte("- just\n- a\n- list\n"))
	f.Add([]byte("just a scalar"))
	f.Add([]byte("zarf_cache:\n  - a\n  - b\n"))
	f.Add([]byte("unknown_field: true\n"))
	f.Add([]byte("distro:\n  create:\n    registry_override:\n      docker.io: example.com\n"))
	f.Add([]byte("age: &x\n  identity: *x\n"))

	f.Fuzz(func(_ *testing.T, data []byte) {
		var cfg types.DistroConfig
		_ = utils.ReadByteStrict(data, &cfg) //nolint:errcheck // no-panic fuzz test, malformed input is expected to error
	})
}
