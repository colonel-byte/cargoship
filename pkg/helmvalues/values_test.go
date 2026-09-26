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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/k0sproject/dig"
	"github.com/stretchr/testify/require"
)

func TestValuePathParsing(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		val     any
		want    map[string]any
		wantErr string
	}{
		{
			name: "index deep in a path",
			path: ".a.b[0].c",
			val:  int64(1),
			want: map[string]any{
				"a": map[string]any{"b": []any{map[string]any{"c": int64(1)}}},
			},
		},
		{
			// Helm leaves the positions before the one that was set empty.
			name: "skipped positions are nil",
			path: ".tags[2]",
			val:  "c",
			want: map[string]any{"tags": []any{nil, nil, "c"}},
		},
		{
			name: "nested list indices",
			path: ".grid[0][1]",
			val:  "x",
			want: map[string]any{"grid": []any{[]any{nil, "x"}}},
		},
		{
			name: "escaped dot stays in the key",
			path: `.annotations.example\.com/team`,
			val:  "infra",
			want: map[string]any{
				"annotations": map[string]any{"example.com/team": "infra"},
			},
		},
		{
			name: "escaped bracket stays in the key",
			path: `.a\[0\]`,
			val:  "x",
			want: map[string]any{"a[0]": "x"},
		},
		{
			// Helm would drop this backslash and hand the chart the key "atemp".
			name: "backslash before a plain character is kept",
			path: `.a\temp`,
			val:  "x",
			want: map[string]any{`a\temp`: "x"},
		},
		{
			name:    "index beyond the maximum",
			path:    ".a[64]",
			val:     "x",
			wantErr: "exceeds the maximum of 63",
		},
		{
			name:    "index is not a number",
			path:    ".a[x]",
			val:     int64(1),
			wantErr: `invalid list index "x"`,
		},
		{
			name:    "unclosed index",
			path:    ".a[0",
			val:     int64(1),
			wantErr: "expected a [index]",
		},
		{
			name:    "empty key",
			path:    ".a..b",
			val:     int64(1),
			wantErr: "empty key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := map[string]any{}
			err := SetValuePath(dst, tt.path, tt.val)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, dst)
		})
	}
}

// A path that walks through a value of the wrong shape is reported rather than
// overwriting what is already there.
func TestValuePathConflicts(t *testing.T) {
	tests := []struct {
		name    string
		dst     map[string]any
		path    string
		wantErr string
	}{
		{
			name:    "scalar in the middle of a path",
			dst:     map[string]any{"a": int64(1)},
			path:    ".a.b",
			wantErr: `path conflict at key "a"`,
		},
		{
			name:    "index into a scalar",
			dst:     map[string]any{"a": int64(1)},
			path:    ".a[0]",
			wantErr: `path conflict at key "a": int64 is not a list`,
		},
		{
			name:    "map key under a list",
			dst:     map[string]any{"a": []any{int64(1)}},
			path:    ".a.b",
			wantErr: `path conflict at key "a": []interface {} is not a map`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SetValuePath(tt.dst, tt.path, int64(2))
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

// typedScalar types the output of a rendered template, so these rules decide
// what a chart receives for "{{ .Values.x }}".
func TestTypedScalar(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   string
		want any
	}{
		{name: "booleans are case insensitive", in: "TRUE", want: true},
		{name: "false", in: "False", want: false},
		{name: "null becomes nil", in: "NULL", want: nil},
		{name: "integer", in: "3", want: int64(3)},
		{name: "bare zero is a number", in: "0", want: int64(0)},
		// Helm does not parse floats, and parsing them would turn the version
		// "1.10" into 1.1 and drop a digit.
		{name: "decimals stay strings", in: "1.10", want: "1.10"},
		// Image tags, zip codes and account numbers all live in chart values.
		{name: "leading zero stays a string", in: "0755", want: "0755"},
		{name: "empty stays empty", in: "", want: ""},
		{name: "plain string", in: "nginx", want: "nginx"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, typedScalar(tt.in))
		})
	}
}

func TestMergeValues(t *testing.T) {
	dst := map[string]any{
		"a": "orig",
		"nested": map[string]any{
			"k1": "v1",
			"k2": "v2",
		},
	}
	src := map[string]any{
		"nested": map[string]any{
			"k2": "updated",
			"k3": "v3",
		},
		"b": 100,
	}

	merged := MergeValues(dst, src)
	require.Equal(t, map[string]any{
		"a": "orig",
		"nested": map[string]any{
			"k1": "v1",
			"k2": "updated",
			"k3": "v3",
		},
		"b": 100,
	}, merged)
}

// Values files are merged repeatedly while composing a package, so a merge that
// wrote through to its inputs would leak one result into every later merge.
func TestMergeValuesDoesNotMutateItsArguments(t *testing.T) {
	dst := map[string]any{"nested": map[string]any{"k1": "v1"}}
	src := map[string]any{"nested": map[string]any{"k2": "v2"}}

	merged := MergeValues(dst, src)

	require.Equal(t, map[string]any{"nested": map[string]any{"k1": "v1"}}, dst)
	require.Equal(t, map[string]any{"nested": map[string]any{"k2": "v2"}}, src)

	// The result must not alias either input either.
	mergedNested, ok := merged["nested"].(map[string]any)
	require.True(t, ok)
	mergedNested["k3"] = "v3"
	require.NotContains(t, dst["nested"], "k3")
	require.NotContains(t, src["nested"], "k3")
}

// A nested map from a YAML decoder or from dig must deep-merge rather than have
// its whole branch replaced.
func TestMergeValuesNormalizesNestedMapTypes(t *testing.T) {
	tests := []struct {
		name string
		dst  map[string]any
		src  map[string]any
	}{
		{
			name: "dig.Mapping on the src side",
			dst:  map[string]any{"nested": map[string]any{"keep": "yes"}},
			src:  map[string]any{"nested": dig.Mapping{"add": "yes"}},
		},
		{
			name: "dig.Mapping on the dst side",
			dst:  map[string]any{"nested": dig.Mapping{"keep": "yes"}},
			src:  map[string]any{"nested": map[string]any{"add": "yes"}},
		},
		{
			name: "map[any]any on the src side",
			dst:  map[string]any{"nested": map[string]any{"keep": "yes"}},
			src:  map[string]any{"nested": map[any]any{"add": "yes"}},
		},
		{
			name: "map[any]any on both sides",
			dst:  map[string]any{"nested": map[any]any{"keep": "yes"}},
			src:  map[string]any{"nested": map[any]any{"add": "yes"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, map[string]any{
				"nested": map[string]any{"keep": "yes", "add": "yes"},
			}, MergeValues(tt.dst, tt.src))
		})
	}
}

func TestMergeValuesDeepCopiesSlices(t *testing.T) {
	src := map[string]any{"list": []any{map[string]any{"k": "v"}}}

	merged := MergeValues(nil, src)

	mergedList, ok := merged["list"].([]any)
	require.True(t, ok)
	mergedItem, ok := mergedList[0].(map[string]any)
	require.True(t, ok)
	mergedItem["k"] = "changed"

	srcList, ok := src["list"].([]any)
	require.True(t, ok)
	srcItem, ok := srcList[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "v", srcItem["k"])
}

func TestRenderValues(t *testing.T) {
	t.Run("passthrough string", func(t *testing.T) {
		out, err := RenderValues("foo: bar\n")
		require.NoError(t, err)
		require.Equal(t, "foo: bar\n", out)
	})

	t.Run("render map", func(t *testing.T) {
		out, err := RenderValues(map[string]any{"foo": map[string]any{"bar": "baz"}})
		require.NoError(t, err)
		require.Equal(t, "foo:\n  bar: baz\n", out)
	})
}

func TestTemplateRendering(t *testing.T) {
	data := map[string]any{
		"Distro": map[string]any{
			"Version": "v1.31.0",
		},
		"Name": "cargoship",
	}

	t.Run("render template string with sprout funcs", func(t *testing.T) {
		tmpl := "name: {{ .Name | upper }}\nversion: {{ .Distro.Version }}"
		out, err := RenderTemplate(tmpl, data)
		require.NoError(t, err)
		require.Equal(t, "name: CARGOSHIP\nversion: v1.31.0", out)
	})

	t.Run("recursive template evaluation in nested map", func(t *testing.T) {
		nested := map[string]any{
			"app": "{{ .Name | upper }}",
			"details": map[string]any{
				"tag": "{{ .Distro.Version }}",
			},
		}
		evaled, err := EvaluateValuesTemplates(nested, data)
		require.NoError(t, err)
		require.Equal(t, map[string]any{
			"app": "CARGOSHIP",
			"details": map[string]any{
				"tag": "v1.31.0",
			},
		}, evaled)
	})

	t.Run("rendered scalars use helm typing", func(t *testing.T) {
		for _, tt := range []struct {
			in   string
			want any
		}{
			{"1.10", "1.10"},
			{"0123456789", "0123456789"},
			{"3", int64(3)},
			{"true", true},
			{"plain", "plain"},
		} {
			got, err := EvaluateValuesTemplates("{{ .V }}", map[string]any{"V": tt.in})
			require.NoError(t, err)
			require.Equal(t, tt.want, got, "input %q", tt.in)
		}
	})

	t.Run("rendered list syntax stays a string", func(t *testing.T) {
		got, err := EvaluateValuesTemplates("{{ .V }}", map[string]any{"V": "[a,b]"})
		require.NoError(t, err)
		require.Equal(t, "[a,b]", got)
	})

	t.Run("untemplated strings keep their yaml type", func(t *testing.T) {
		got, err := EvaluateValuesTemplates("0755", nil)
		require.NoError(t, err)
		require.Equal(t, "0755", got)
	})

	// Trimming for type detection must not reach the value that is handed back,
	// or a toYaml block loses its trailing newline.
	t.Run("whitespace in untyped output is preserved", func(t *testing.T) {
		got, err := EvaluateValuesTemplates("{{ .V }}", map[string]any{
			"V": "limits:\n  cpu: 100m\n",
		})
		require.NoError(t, err)
		require.Equal(t, "limits:\n  cpu: 100m\n", got)
	})

	t.Run("surrounding whitespace still allows typing", func(t *testing.T) {
		got, err := EvaluateValuesTemplates("{{ .V }}", map[string]any{"V": "  3  "})
		require.NoError(t, err)
		require.Equal(t, int64(3), got)
	})
}

// A typo in a value path renders as the literal "<no value>", which would
// otherwise be written into a chart as if it were the intended value.
func TestMissingKeyIsAnError(t *testing.T) {
	data := map[string]any{"Values": map[string]any{"present": "yes"}}

	t.Run("bare missing key", func(t *testing.T) {
		_, err := RenderTemplate("{{ .Values.absent }}", data)
		require.ErrorContains(t, err, "resolved a missing key")
	})

	t.Run("missing key nested in values", func(t *testing.T) {
		_, err := EvaluateValuesTemplates(map[string]any{"tag": "{{ .Values.absent }}"}, data)
		require.ErrorContains(t, err, "resolved a missing key")
	})

	// missingkey=error would reject all of these, which is why the output is
	// checked instead. They are how a Helm user makes a value optional.
	t.Run("optional value idioms still work", func(t *testing.T) {
		for tmpl, want := range map[string]string{
			`{{ .Values.absent | default "fallback" }}`:          "fallback",
			`{{ if .Values.absent }}set{{ else }}unset{{ end }}`: "unset",
			`{{ with .Values.absent }}{{ . }}{{ end }}`:          "",
			`{{ if empty .Values.absent }}is-empty{{ end }}`:     "is-empty",
			`{{ .Values.present }}`:                              "yes",
		} {
			out, err := RenderTemplate(tmpl, data)
			require.NoError(t, err, "template %s", tmpl)
			require.Equal(t, want, out, "template %s", tmpl)
		}
	})

	t.Run("long templates are truncated in the error", func(t *testing.T) {
		_, err := RenderTemplate("{{ .Values.absent }}"+strings.Repeat("x", 500), data)
		require.ErrorContains(t, err, "...")
		require.Less(t, len(err.Error()), 300)
	})
}

// Functions that read host state are removed from the map rather than stubbed, so
// naming one is a parse error a package author cannot miss.
func TestDeniedFuncsAreNotAvailable(t *testing.T) {
	for _, name := range deniedFuncs {
		t.Run(name, func(t *testing.T) {
			require.NotContains(t, FuncMap(), name)
			require.NotContains(t, FuncMap(WithCreateFuncs(t.Context())), name)

			_, err := RenderTemplate(`{{ `+name+` "HOME" }}`, nil)
			require.ErrorContains(t, err, "not defined")
		})
	}
}

// The sprig aliases are the reason sprigin is used instead of sprout's raw
// registries, so check a representative few survived the deletions.
func TestSprigAliasesSurvive(t *testing.T) {
	fm := FuncMap()
	for _, name := range []string{"upper", "lower", "toYaml", "fromYaml", "b64enc", "default", "trimAll", "semverCompare"} {
		require.Contains(t, fm, name, "sprig alias %q missing", name)
	}
}

func TestCreateFuncsAreScoped(t *testing.T) {
	tmpl := `{{ fileExists "/nonexistent/path/for/sure" }}`

	t.Run("absent by default", func(t *testing.T) {
		_, err := RenderTemplate(tmpl, nil)
		require.ErrorContains(t, err, `function "fileExists" not defined`)
	})

	t.Run("present with WithCreateFuncs", func(t *testing.T) {
		out, err := RenderTemplate(tmpl, nil, WithCreateFuncs(t.Context()))
		require.NoError(t, err)
		require.Equal(t, "false", out)
	})

	t.Run("option reaches nested values", func(t *testing.T) {
		nested := map[string]any{"exists": `{{ fileExists "/nonexistent/path/for/sure" }}`}

		_, err := EvaluateValuesTemplates(nested, nil)
		require.ErrorContains(t, err, `function "fileExists" not defined`)

		got, err := EvaluateValuesTemplates(nested, nil, WithCreateFuncs(t.Context()))
		require.NoError(t, err)
		require.Equal(t, map[string]any{"exists": false}, got)
	})

	t.Run("all create funcs are gated", func(t *testing.T) {
		for _, name := range []string{"fileExists", "cachedFileExists", "cachedFilePath", "downloadToCache"} {
			require.NotContains(t, FuncMap(), name)
			require.Contains(t, FuncMap(WithCreateFuncs(t.Context())), name)
		}
	})
}

func TestRenderValuesSlicesAndScalars(t *testing.T) {
	t.Run("slice at the top level", func(t *testing.T) {
		out, err := RenderValues([]any{"a", int64(1), map[string]any{"k": "v"}})
		require.NoError(t, err)
		require.Equal(t, "- a\n- 1\n- k: v\n", out)
	})

	t.Run("nested slice keeps two-space indent", func(t *testing.T) {
		out, err := RenderValues(map[string]any{"tags": []any{"a", "b"}})
		require.NoError(t, err)
		require.Equal(t, "tags:\n  - a\n  - b\n", out)
	})

	t.Run("scalar", func(t *testing.T) {
		out, err := RenderValues(int64(3))
		require.NoError(t, err)
		require.Equal(t, "3\n", out)
	})

	t.Run("nil", func(t *testing.T) {
		out, err := RenderValues(nil)
		require.NoError(t, err)
		require.Equal(t, "null\n", out)
	})
}

// dig.Mapping is what the cluster config decodes into, so it is the input shape
// EvaluateValuesTemplates sees in practice. The type is preserved on the way out
// because a caller hands the result straight back to dig.
func TestEvaluateValuesTemplatesOnDigMapping(t *testing.T) {
	in := dig.Mapping{
		"{{ .Key }}": dig.Mapping{
			"replicas": "{{ .N }}",
			"tags":     []any{"{{ .Key }}-a", "plain"},
		},
	}
	data := map[string]any{"Key": "chart", "N": "3"}

	got, err := EvaluateValuesTemplates(in, data)
	require.NoError(t, err)
	require.Equal(t, dig.Mapping{
		"chart": dig.Mapping{
			"replicas": int64(3),
			"tags":     []any{"chart-a", "plain"},
		},
	}, got)
}

func TestEvaluateValuesTemplatesOnMapAnyAny(t *testing.T) {
	in := map[any]any{"a": "{{ .V }}", 1: "keyed by int"}

	got, err := EvaluateValuesTemplates(in, map[string]any{"V": "true"})
	require.NoError(t, err)
	require.Equal(t, map[string]any{"a": true, "1": "keyed by int"}, got)
}

func TestCachedFilePathFunc(t *testing.T) {
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)

	out, err := RenderTemplate(`{{ cachedFilePath "charts/foo.tgz" }}`, nil, WithCreateFuncs(t.Context()))
	require.NoError(t, err)
	require.Equal(t, filepath.Join(cacheHome, "cargoship", "charts/foo.tgz"), out)
}

func TestCachedFileExistsFunc(t *testing.T) {
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)

	tmpl := `{{ cachedFileExists "charts/foo.tgz" }}`

	out, err := RenderTemplate(tmpl, nil, WithCreateFuncs(t.Context()))
	require.NoError(t, err)
	require.Equal(t, "false", out)

	target := filepath.Join(cacheHome, "cargoship", "charts", "foo.tgz")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, []byte("chart"), 0o644))

	out, err = RenderTemplate(tmpl, nil, WithCreateFuncs(t.Context()))
	require.NoError(t, err)
	require.Equal(t, "true", out)
}

func TestDownloadToCacheFunc(t *testing.T) {
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("payload")); err != nil {
			t.Errorf("writing test response: %v", err)
		}
	}))
	defer ts.Close()

	tmpl := `{{ downloadToCache "` + ts.URL + `" "charts/foo.tgz" }}`
	out, err := RenderTemplate(tmpl, nil, WithCreateFuncs(t.Context()))
	require.NoError(t, err)

	want := filepath.Join(cacheHome, "cargoship", "charts/foo.tgz")
	require.Equal(t, want, out)
	content, err := os.ReadFile(want)
	require.NoError(t, err)
	require.Equal(t, "payload", string(content))

	// A checksum that does not match must stop the render rather than render an
	// empty string and let the package carry on with a missing artifact.
	bad := `{{ downloadToCache "` + ts.URL + `" "charts/bar.tgz" "deadbeef" }}`
	_, err = RenderTemplate(bad, nil, WithCreateFuncs(t.Context()))
	require.ErrorContains(t, err, "checksum mismatch")
}

// A cache that cannot be read must not look like a cache miss, or a package
// takes the "download it again" branch on a permission problem and reports
// something unrelated when that fails too.
func TestFileExistsSurfacesNonNotExistErrors(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	dir := filepath.Join(t.TempDir(), "locked")
	require.NoError(t, os.Mkdir(dir, 0o000))
	// t.TempDir cannot remove the directory until it is readable again.
	t.Cleanup(func() {
		require.NoError(t, os.Chmod(dir, 0o755))
	})

	tmpl := `{{ fileExists "` + filepath.Join(dir, "file.txt") + `" }}`
	_, err := RenderTemplate(tmpl, nil, WithCreateFuncs(t.Context()))
	require.ErrorContains(t, err, "permission denied")
}

// ZEP-0021 writes every sourcePath and targetPath with a leading dot, and a lone
// dot for the root.
func TestSetValuePath(t *testing.T) {
	t.Run("nested path", func(t *testing.T) {
		dst := map[string]any{}
		require.NoError(t, SetValuePath(dst, ".image.tag", "v1.31.0"))
		require.Equal(t, map[string]any{
			"image": map[string]any{"tag": "v1.31.0"},
		}, dst)
	})

	t.Run("list index", func(t *testing.T) {
		dst := map[string]any{}
		require.NoError(t, SetValuePath(dst, ".servers[1].port", int64(80)))
		require.Equal(t, map[string]any{
			"servers": []any{nil, map[string]any{"port": int64(80)}},
		}, dst)
	})

	t.Run("root merges instead of replacing", func(t *testing.T) {
		dst := map[string]any{
			"keep":  "me",
			"image": map[string]any{"tag": "old", "pullPolicy": "Always"},
		}
		src := map[string]any{"image": map[string]any{"tag": "new"}}

		require.NoError(t, SetValuePath(dst, ".", src))
		require.Equal(t, map[string]any{
			"keep":  "me",
			"image": map[string]any{"tag": "new", "pullPolicy": "Always"},
		}, dst)
	})

	t.Run("root accepts a dig.Mapping", func(t *testing.T) {
		dst := map[string]any{}
		require.NoError(t, SetValuePath(dst, ".", dig.Mapping{"a": "b"}))
		require.Equal(t, map[string]any{"a": "b"}, dst)
	})

	t.Run("missing leading dot", func(t *testing.T) {
		err := SetValuePath(map[string]any{}, "image.tag", "v1")
		require.ErrorContains(t, err, "must start with a dot")
	})

	t.Run("root value must be a map", func(t *testing.T) {
		err := SetValuePath(map[string]any{}, ".", "scalar")
		require.ErrorContains(t, err, "must be a map, not string")
	})

	t.Run("path conflict", func(t *testing.T) {
		dst := map[string]any{"image": "nginx"}
		err := SetValuePath(dst, ".image.tag", "v1")
		require.ErrorContains(t, err, `path conflict at key "image"`)
	})
}

func TestGetValuePath(t *testing.T) {
	src := map[string]any{
		"image": dig.Mapping{"tag": "v1.31.0"},
		"servers": []any{
			map[string]any{"port": int64(80)},
		},
		"replicas": int64(3),
	}

	t.Run("present", func(t *testing.T) {
		for path, want := range map[string]any{
			".image.tag":       "v1.31.0",
			".servers[0].port": int64(80),
			".replicas":        int64(3),
		} {
			got, ok, err := GetValuePath(src, path)
			require.NoError(t, err, path)
			require.True(t, ok, path)
			require.Equal(t, want, got, path)
		}
	})

	t.Run("root returns everything", func(t *testing.T) {
		got, ok, err := GetValuePath(src, ".")
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, src, got)
	})

	// An unset sourcePath is absent, not an error, so one optional value does not
	// fail the whole mapping.
	t.Run("absent", func(t *testing.T) {
		for _, path := range []string{
			".missing",
			".image.missing",
			".replicas.tag",
			".servers[9].port",
			".image[0]",
		} {
			got, ok, err := GetValuePath(src, path)
			require.NoError(t, err, path)
			require.False(t, ok, path)
			require.Nil(t, got, path)
		}
	})

	t.Run("missing leading dot", func(t *testing.T) {
		_, _, err := GetValuePath(src, "image.tag")
		require.ErrorContains(t, err, "must start with a dot")
	})

	t.Run("malformed index", func(t *testing.T) {
		_, _, err := GetValuePath(src, ".servers[x]")
		require.ErrorContains(t, err, `invalid list index "x"`)
	})
}
