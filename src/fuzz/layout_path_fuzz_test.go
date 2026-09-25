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

	"github.com/colonel-byte/cargoship/src/pkg/packager/layout"
	"github.com/stretchr/testify/require"
)

// FuzzIsContainedPathStaysInside asserts that IsContainedPath never panics, and that every string
// it accepts really does resolve inside a directory rather than escaping it.
//
// The values it guards -- a package's values files and values schema path -- are named by a
// distro.yaml that travels inside the package and can arrive over an OCI pull, so the string
// reaching this check is not one cargoship wrote. Accepting a path that escapes the package
// directory is a path-traversal vulnerability: the caller joins whatever this approves onto a real
// directory and reads or writes there.
func FuzzIsContainedPathStaysInside(f *testing.F) {
	f.Add("values.yaml")
	f.Add("nested/values.yaml")
	f.Add("../escape.yaml")
	f.Add("..")
	f.Add("")
	f.Add("/etc/passwd")
	f.Add(`C:\Windows\System32`)
	f.Add("a/../../b")
	f.Add("a/b/../../../c")
	f.Add("https://example.com/values.yaml")
	f.Add("oci://ghcr.io/example/package:v1.0.0")
	f.Add(strings.Repeat("../", 50) + "etc/passwd")
	f.Add(`nested\..\..\escape.yaml`)

	f.Fuzz(func(t *testing.T, s string) {
		if !layout.IsContainedPath(s) {
			return
		}

		base := t.TempDir()
		resolved := filepath.Join(base, filepath.FromSlash(s))

		rel, err := filepath.Rel(base, resolved)
		require.NoError(t, err, "accepted path %q could not be related back to its base", s)
		require.False(t, rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)),
			"accepted path %q resolves outside its base directory: %q", s, resolved)
	})
}

// FuzzIsCleanPathNoPanic asserts that IsCleanPath never panics, and that every string it accepts
// contains no path separator and is not a bare parent-directory reference.
//
// It guards a package's metadata name and version, which name a directory or file component
// directly rather than a multi-segment path, so the property to hold is narrower than
// IsContainedPath's: nothing that could turn one path segment into two or more.
func FuzzIsCleanPathNoPanic(f *testing.F) {
	f.Add("v1.0.0")
	f.Add("my-distro")
	f.Add("..")
	f.Add("../escape")
	f.Add("a/b")
	f.Add(`a\b`)
	f.Add("")
	f.Add(".")
	f.Add("...")

	f.Fuzz(func(t *testing.T, s string) {
		if !layout.IsCleanPath(s) {
			return
		}
		require.NotEqual(t, "..", s, "accepted a bare parent-directory reference")
		require.False(t, strings.ContainsAny(s, `/\`), "accepted %q, which carries a path separator", s)
	})
}
