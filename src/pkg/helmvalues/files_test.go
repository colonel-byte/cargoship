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

package helmvalues

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// write creates a file under dir and returns its path.
func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

func TestParseFile(t *testing.T) {
	dir := t.TempDir()

	t.Run("types come from YAML", func(t *testing.T) {
		p := write(t, dir, "values.yaml", "replicas: 3\nenabled: true\ntag: \"0755\"\nratio: 1.5\n")
		got, err := ParseFile(p)
		require.NoError(t, err)
		require.Equal(t, map[string]any{
			"replicas": 3,
			"enabled":  true,
			"tag":      "0755",
			// A values file yields a real float, unlike a rendered template.
			"ratio": 1.5,
		}, got)
	})

	t.Run("nested maps are normalized", func(t *testing.T) {
		p := write(t, dir, "nested.yaml", "image:\n  tag: v1\n")
		got, err := ParseFile(p)
		require.NoError(t, err)
		require.Equal(t, map[string]any{"image": map[string]any{"tag": "v1"}}, got)
	})

	t.Run("empty file is an empty map", func(t *testing.T) {
		p := write(t, dir, "empty.yaml", "# only a comment\n")
		got, err := ParseFile(p)
		require.NoError(t, err)
		require.Equal(t, map[string]any{}, got)
	})

	t.Run("yml extension is accepted", func(t *testing.T) {
		p := write(t, dir, "alt.yml", "a: b\n")
		got, err := ParseFile(p)
		require.NoError(t, err)
		require.Equal(t, map[string]any{"a": "b"}, got)
	})

	t.Run("non-YAML extension is rejected", func(t *testing.T) {
		p := write(t, dir, "values.json", `{"a":"b"}`)
		_, err := ParseFile(p)
		require.ErrorContains(t, err, "must be YAML")
	})

	t.Run("top-level list is rejected", func(t *testing.T) {
		p := write(t, dir, "list.yaml", "- a\n- b\n")
		_, err := ParseFile(p)
		require.ErrorContains(t, err, "must hold a mapping at its top level")
	})

	t.Run("malformed YAML is rejected", func(t *testing.T) {
		p := write(t, dir, "bad.yaml", "a: [1,\n")
		_, err := ParseFile(p)
		require.ErrorContains(t, err, "parsing values file")
	})

	t.Run("missing file is reported", func(t *testing.T) {
		_, err := ParseFile(filepath.Join(dir, "absent.yaml"))
		require.ErrorContains(t, err, "reading values file")
	})
}

func TestLoadFiles(t *testing.T) {
	ctx := context.Background()

	t.Run("later file wins key by key", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "base.yaml", "image:\n  tag: old\n  pullPolicy: Always\nkeep: me\n")
		write(t, dir, "over.yaml", "image:\n  tag: new\n")

		got, err := LoadFiles(ctx, dir, "", []string{"base.yaml", "over.yaml"})
		require.NoError(t, err)
		require.Equal(t, map[string]any{
			"keep":  "me",
			"image": map[string]any{"tag": "new", "pullPolicy": "Always"},
		}, got)
	})

	t.Run("no files is an empty map", func(t *testing.T) {
		got, err := LoadFiles(ctx, t.TempDir(), "", nil)
		require.NoError(t, err)
		require.Equal(t, map[string]any{}, got)
	})

	t.Run("relative paths resolve against baseDir", func(t *testing.T) {
		dir := t.TempDir()
		sub := filepath.Join(dir, "conf")
		require.NoError(t, os.Mkdir(sub, 0o755))
		write(t, sub, "values.yaml", "a: b\n")

		got, err := LoadFiles(ctx, dir, "", []string{filepath.Join("conf", "values.yaml")})
		require.NoError(t, err)
		require.Equal(t, map[string]any{"a": "b"}, got)
	})

	t.Run("absolute paths are taken as given", func(t *testing.T) {
		dir := t.TempDir()
		p := write(t, dir, "values.yaml", "a: b\n")

		got, err := LoadFiles(ctx, t.TempDir(), "", []string{p})
		require.NoError(t, err)
		require.Equal(t, map[string]any{"a": "b"}, got)
	})

	t.Run("a bad file names itself", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "good.yaml", "a: b\n")
		write(t, dir, "bad.yaml", "- list\n")

		_, err := LoadFiles(ctx, dir, "", []string{"good.yaml", "bad.yaml"})
		require.ErrorContains(t, err, "bad.yaml")
	})
}

func TestLoadFilesFromURL(t *testing.T) {
	ctx := context.Background()
	// The handler runs on the server's goroutine, so it reports write failures
	// with Errorf rather than a require that would call FailNow off the test
	// goroutine.
	serve := func(w http.ResponseWriter, body string) {
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("failed to write response: %v", err)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/values.yaml":
			serve(w, "remote: yes\nshared: from-url\n")
		case "/notes.txt":
			serve(w, "shared: from-txt\n")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	t.Run("merges after a local file", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "local.yaml", "shared: from-local\nlocal: yes\n")

		got, err := LoadFiles(ctx, dir, "", []string{"local.yaml", srv.URL + "/values.yaml"})
		require.NoError(t, err)
		// "yes" stays a string: yaml.v3 follows YAML 1.2, where only true/false
		// are booleans.
		require.Equal(t, map[string]any{
			"local":  "yes",
			"remote": "yes",
			"shared": "from-url",
		}, got)
	})

	t.Run("downloads land in the given temp dir", func(t *testing.T) {
		tmp := t.TempDir()
		_, err := LoadFiles(ctx, t.TempDir(), tmp, []string{srv.URL + "/values.yaml"})
		require.NoError(t, err)

		entries, err := os.ReadDir(tmp)
		require.NoError(t, err)
		require.Len(t, entries, 1)
		require.Equal(t, "0-values.yaml", entries[0].Name())
	})

	t.Run("non-YAML URL is rejected before it is fetched", func(t *testing.T) {
		_, err := LoadFiles(ctx, t.TempDir(), "", []string{srv.URL + "/notes.txt"})
		require.ErrorContains(t, err, "must be YAML")
	})

	t.Run("URL without a file name is rejected", func(t *testing.T) {
		_, err := LoadFiles(ctx, t.TempDir(), "", []string{srv.URL + "/"})
		require.ErrorContains(t, err, "has no file name")
	})

	t.Run("a failed download is reported", func(t *testing.T) {
		_, err := LoadFiles(ctx, t.TempDir(), "", []string{srv.URL + "/missing.yaml"})
		require.ErrorContains(t, err, "downloading values file")
	})
}
