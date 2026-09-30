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
	"path/filepath"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/pkg/fileoverride"
	"github.com/stretchr/testify/require"
)

// FuzzParseFileOverrides asserts that fileoverride.Parse, which turns repeated --file-override
// flags into a structured, deduped, sorted list, never panics and that on success its stated
// invariants hold: no two entries share a Source, entries are sorted so the longest Source
// sorts first, and a local target has been resolved to an absolute path.
//
// The fuzzed string is split on newlines into the flag's repeated-value form, which is how
// []string-typed CLI input is reached from the types fuzzing supports.
func FuzzParseFileOverrides(f *testing.F) {
	f.Add("https://github.com/rancher=https://mirror.example.com/r\nhttps://github.com=https://mirror.example.com/gh")
	f.Add("https://rpm.rancher.io=/srv/staged")
	f.Add("https://a.example.com=https://b.example.com\nhttps://a.example.com=https://c.example.com")
	f.Add("missing-equals")
	f.Add("=https://missing-source.example.com")
	f.Add("https://missing-target.example.com=")
	f.Add("")
	f.Add("=")
	f.Add("https://a.example.com=b=c")
	f.Add("oci://a.example.com=https://b.example.com")
	f.Add(strings.Repeat("https://x.example.com=y\n", 50))

	f.Fuzz(func(t *testing.T, overrides string) {
		var lines []string
		if overrides != "" {
			lines = strings.Split(overrides, "\n")
		}

		result, err := fileoverride.Parse(lines)
		if err != nil {
			return
		}

		seen := make(map[string]bool, len(result))
		for i, o := range result {
			require.NotEmpty(t, o.Source, "entry %d has an empty source", i)
			require.NotEmpty(t, o.Target, "entry %d has an empty target", i)
			require.False(t, seen[o.Source], "source %q appears more than once in the result", o.Source)
			seen[o.Source] = true
			if o.IsLocal() {
				require.True(t, filepath.IsAbs(o.LocalDir),
					"entry %d has a relative local dir %q, which would be re-resolved against the distro path", i, o.LocalDir)
			}
			if i > 0 {
				require.GreaterOrEqual(t, result[i-1].Source, o.Source,
					"result is not sorted in descending order by source: %q before %q", result[i-1].Source, o.Source)
			}
		}
	})
}

// FuzzResolveFileOverride asserts that Resolve never panics on arbitrary sources and, crucially,
// that a local override never resolves outside the directory it names. url.Path is
// percent-decoded, so a traversal can arrive in forms the raw URL does not show.
func FuzzResolveFileOverride(f *testing.F) {
	f.Add("https://rpm.rancher.io/public/k3s.rpm")
	f.Add("https://rpm.rancher.io/../escaped")
	f.Add("https://rpm.rancher.io/%2e%2e/%2e%2e/escaped")
	f.Add("https://rpm.rancher.io/a/../../escaped")
	f.Add("https://rpm.rancher.io")
	f.Add("https://rpm.rancher.io.example.invalid/public/k3s.rpm")
	f.Add("./not-a-url")
	f.Add("")

	f.Fuzz(func(t *testing.T, src string) {
		localDir := filepath.Join(t.TempDir(), "staged")
		overrides, err := fileoverride.Parse([]string{
			"https://rpm.rancher.io=" + localDir,
			"https://github.com=https://mirror.example.com/gh",
		})
		require.NoError(t, err)

		match, ok, err := fileoverride.Resolve(overrides, src)
		if err != nil || !ok {
			return
		}

		if match.Override.IsLocal() {
			rel, relErr := filepath.Rel(match.Override.LocalDir, match.Resolved)
			require.NoError(t, relErr)
			require.False(t, rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)),
				"resolved %q escapes the override directory %q", match.Resolved, match.Override.LocalDir)
		} else {
			require.True(t, strings.HasPrefix(match.Resolved, match.Override.Target),
				"resolved %q does not start with the override target %q", match.Resolved, match.Override.Target)
		}
	})
}
