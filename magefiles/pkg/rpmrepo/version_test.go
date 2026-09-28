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

package rpmrepo

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompareValues(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"1.0", "1.0", 0},
		{"1.0", "1.1", -1},
		{"1.1", "1.0", 1},
		{"2.16.16", "2.16.16", 0},
		{"2.16.16", "2.16.17", -1},
		{"2.11", "2.11", 0},
		{"2.el10_2.1", "2.el10", 1},
		{"2.el10", "2.el10_2.1", -1},
		{"16.el10", "16.el10", 0},
		// A tilde sorts before anything, including the empty string that follows it -- which is
		// what makes a release candidate older than the release.
		{"1.0~rc1", "1.0", -1},
		{"1.0", "1.0~rc1", 1},
		// A caret sorts after, the inverse of a tilde: a post-release snapshot is newer.
		{"1.0^20240101", "1.0", 1},
		{"1.0", "1.0^20240101", -1},
		// Leading zeroes are not significant, and a longer run of digits is the larger number
		// whatever the lexical comparison would say.
		{"1.007", "1.7", 0},
		{"1.10", "1.9", 1},
		// Digits outrank letters, so 1.1 is newer than 1.1a's alphabetic segment.
		{"1.1", "1.1a", -1},
		// Separators are not compared, only the segments between them.
		{"1.0.1", "1_0-1", 0},
	} {
		require.Equalf(t, tc.want, CompareValues(tc.a, tc.b), "CompareValues(%q, %q)", tc.a, tc.b)
	}
}

func TestVersionCompare(t *testing.T) {
	v1 := Version{Epoch: "1", Ver: "2.16.16", Rel: "2.el10"}
	v2 := Version{Epoch: "1", Ver: "2.16.16", Rel: "2.el10_2.1"}
	v3 := Version{Epoch: "2", Ver: "1.0", Rel: "1"}

	// Equal epoch and version, so the release breaks the tie.
	require.Negative(t, v1.Compare(v2))
	require.Positive(t, v2.Compare(v1))

	// A higher epoch wins outright, even against a much higher version.
	require.Negative(t, v2.Compare(v3))
	require.Positive(t, v3.Compare(v2))

	require.Zero(t, v1.Compare(v1))

	// An absent epoch is zero rather than an error, which is how repodata spells "no epoch".
	require.Zero(t, Version{Ver: "1.0", Rel: "1"}.Compare(Version{Epoch: "0", Ver: "1.0", Rel: "1"}))
}

func TestFullVersion(t *testing.T) {
	require.Equal(t, "2.16.16-2.el10",
		Version{Epoch: "1", Ver: "2.16.16", Rel: "2.el10"}.FullVersion())
}
