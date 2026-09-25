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
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/src/pkg/utils"
	"github.com/stretchr/testify/require"
)

// FuzzIdentifySource asserts that IdentifySource never panics and always returns one of its four
// known categories on success, whatever a --source or --confirm target string looks like.
//
// The checks run in a fixed order -- URL scheme, then file extension, then split-archive marker,
// then deployed-package name -- so a string that could plausibly match more than one of them (an
// http URL ending in ".tar", a hostname that also happens to be a valid package name) is decided by
// that order rather than by which branch a caller expects. Pinning the result to the known set
// catches a typo in a category string turning into a silent new source kind nothing downstream
// recognises.
func FuzzIdentifySource(f *testing.F) {
	f.Add("oci://ghcr.io/example/package:v1.0.0")
	f.Add("https://example.com/package.tar.zst")
	f.Add("./local-package.tar")
	f.Add("./local-package.tar.zst")
	f.Add("package.part000")
	f.Add("something.part000.tar")
	f.Add("my-deployed-package")
	f.Add("My-Deployed-Package")
	f.Add("-leading-hyphen")
	f.Add("")
	f.Add("http://")
	f.Add("://not-a-scheme")
	f.Add("relative/path/no/scheme")
	f.Add("C:\\Windows\\path.tar")

	knownSources := map[string]bool{
		"tarball": true,
		"split":   true,
		"cluster": true,
	}

	f.Fuzz(func(t *testing.T, src string) {
		source, err := utils.IdentifySource(src)
		if err != nil {
			require.Empty(t, source, "an error result still carried a source value for %q", src)
			return
		}
		require.NotEmpty(t, source, "a successful result carried no source value for %q", src)
		if knownSources[source] {
			return
		}
		// Anything else is a URL scheme, which url.Parse accepts for a huge range of strings and
		// lowercases per RFC 3986, so the comparison has to fold case the same way.
		require.True(t, len(src) > len(source) && strings.EqualFold(src[:len(source)], source) && src[len(source)] == ':',
			"result %q for %q is not a known category and not a URL scheme prefix of it", source, src)
	})
}
