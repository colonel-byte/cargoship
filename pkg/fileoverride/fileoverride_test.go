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

package fileoverride

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSortsLongestPrefixFirst(t *testing.T) {
	t.Parallel()

	got, err := Parse([]string{
		"https://github.com=https://mirror.example.com/gh",
		"https://github.com/rancher=https://mirror.example.com/rancher",
	})
	require.NoError(t, err)
	require.Equal(t, []Override{
		{
			Source: "https://github.com/rancher",
			Target: "https://mirror.example.com/rancher",
		},
		{
			Source: "https://github.com",
			Target: "https://mirror.example.com/gh",
		},
	}, got)
}

func TestParseEmpty(t *testing.T) {
	t.Parallel()

	got, err := Parse(nil)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestParseResolvesLocalTargetToAbsolutePath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	got, err := Parse([]string{"https://rpm.rancher.io=" + dir})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.True(t, got[0].IsLocal())
	require.Equal(t, dir, got[0].LocalDir)
	require.True(t, filepath.IsAbs(got[0].LocalDir))
}

func TestParseErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []string
	}{
		{
			name:  "missing equals",
			input: []string{"https://github.com"},
		},
		{
			name:  "missing source",
			input: []string{"=https://mirror.example.com"},
		},
		{
			name:  "missing value",
			input: []string{"https://github.com="},
		},
		{
			name: "duplicate source",
			input: []string{
				"https://github.com=https://a.example.com",
				"https://github.com=https://b.example.com",
			},
		},
		{
			name:  "source is not a URL",
			input: []string{"github.com=https://mirror.example.com"},
		},
		{
			name:  "source has a non-http scheme",
			input: []string{"oci://github.com=https://mirror.example.com"},
		},
		{
			name:  "target has a non-http scheme",
			input: []string{"https://github.com=ftp://mirror.example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse(tt.input)
			require.Error(t, err)
		})
	}
}

func TestResolveRemote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []string
		src      string
		want     string
		wantOK   bool
		wantFrom string
	}{
		{
			name:     "bare host prefix rewrites the whole path",
			input:    []string{"https://rpm.rancher.io=https://mirror.example.com/rpm-rancher"},
			src:      "https://rpm.rancher.io/public/centos/9/x86_64/k3s.rpm",
			want:     "https://mirror.example.com/rpm-rancher/public/centos/9/x86_64/k3s.rpm",
			wantOK:   true,
			wantFrom: "https://rpm.rancher.io",
		},
		{
			name: "longest matching prefix wins",
			input: []string{
				"https://github.com=https://mirror.example.com/gh",
				"https://github.com/rancher=https://mirror.example.com/rancher",
			},
			src:      "https://github.com/rancher/rke2/releases/download/v1.0.0/rke2.tar.gz",
			want:     "https://mirror.example.com/rancher/rke2/releases/download/v1.0.0/rke2.tar.gz",
			wantOK:   true,
			wantFrom: "https://github.com/rancher",
		},
		{
			name: "a shorter prefix still catches what the longer one misses",
			input: []string{
				"https://github.com=https://mirror.example.com/gh",
				"https://github.com/rancher=https://mirror.example.com/rancher",
			},
			src:      "https://github.com/k3s-io/k3s/releases/download/v1.0.0/k3s",
			want:     "https://mirror.example.com/gh/k3s-io/k3s/releases/download/v1.0.0/k3s",
			wantOK:   true,
			wantFrom: "https://github.com",
		},
		{
			name:     "a trailing slash on the target does not double up",
			input:    []string{"https://rpm.rancher.io=https://mirror.example.com/rpm/"},
			src:      "https://rpm.rancher.io/k3s.rpm",
			want:     "https://mirror.example.com/rpm/k3s.rpm",
			wantOK:   true,
			wantFrom: "https://rpm.rancher.io",
		},
		{
			name:     "the query and fragment survive the rewrite",
			input:    []string{"https://files.example.com=https://mirror.example.com/files"},
			src:      "https://files.example.com/get?id=7#frag",
			want:     "https://mirror.example.com/files/get?id=7#frag",
			wantOK:   true,
			wantFrom: "https://files.example.com",
		},
		{
			name:     "an exact match rewrites to the target alone",
			input:    []string{"https://files.example.com/blob=https://mirror.example.com/blob"},
			src:      "https://files.example.com/blob",
			want:     "https://mirror.example.com/blob",
			wantOK:   true,
			wantFrom: "https://files.example.com/blob",
		},
		{
			name:   "a lookalike host does not match",
			input:  []string{"https://github.com=https://mirror.example.com/gh"},
			src:    "https://github.com.example.invalid/rancher/rke2",
			wantOK: false,
		},
		{
			name:   "a path prefix only matches on a segment boundary",
			input:  []string{"https://github.com/rancher=https://mirror.example.com/rancher"},
			src:    "https://github.com/rancher-dev/rke2",
			wantOK: false,
		},
		{
			name:   "a different scheme does not match",
			input:  []string{"https://github.com=https://mirror.example.com/gh"},
			src:    "http://github.com/rancher/rke2",
			wantOK: false,
		},
		{
			name:   "a different port does not match",
			input:  []string{"https://files.example.com=https://mirror.example.com"},
			src:    "https://files.example.com:8443/k3s",
			wantOK: false,
		},
		{
			name:   "a non-URL source is never overridden",
			input:  []string{"https://github.com=https://mirror.example.com/gh"},
			src:    "./local/k3s.tar.gz",
			wantOK: false,
		},
		{
			name:   "no overrides configured",
			input:  nil,
			src:    "https://github.com/rancher/rke2",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			overrides, err := Parse(tt.input)
			require.NoError(t, err)

			got, ok, err := Resolve(overrides, tt.src)
			require.NoError(t, err)
			require.Equal(t, tt.wantOK, ok)
			if !tt.wantOK {
				return
			}
			require.Equal(t, tt.want, got.Resolved)
			require.Equal(t, tt.wantFrom, got.Override.Source)
		})
	}
}

func TestResolveLocal(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	overrides, err := Parse([]string{"https://rpm.rancher.io=" + dir})
	require.NoError(t, err)

	got, ok, err := Resolve(overrides, "https://rpm.rancher.io/public/centos/9/k3s.rpm")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, filepath.Join(dir, "public", "centos", "9", "k3s.rpm"), got.Resolved)
	require.True(t, got.Override.IsLocal())
}

func TestResolveLocalRejectsTraversal(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	overrides, err := Parse([]string{"https://rpm.rancher.io=" + filepath.Join(dir, "staged")})
	require.NoError(t, err)

	// url.Path is percent-decoded, so an encoded traversal reaches the join as "..".
	for _, src := range []string{
		"https://rpm.rancher.io/../escaped.rpm",
		"https://rpm.rancher.io/%2e%2e/escaped.rpm",
		"https://rpm.rancher.io/a/../../escaped.rpm",
	} {
		_, ok, err := Resolve(overrides, src)
		require.Error(t, err, src)
		require.False(t, ok, src)
	}
}

func TestToMap(t *testing.T) {
	t.Parallel()

	require.Nil(t, ToMap(nil))

	overrides, err := Parse([]string{
		"https://github.com=https://mirror.example.com/gh",
		"https://rpm.rancher.io=https://mirror.example.com/rpm",
	})
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"https://github.com":     "https://mirror.example.com/gh",
		"https://rpm.rancher.io": "https://mirror.example.com/rpm",
	}, ToMap(overrides))
}
