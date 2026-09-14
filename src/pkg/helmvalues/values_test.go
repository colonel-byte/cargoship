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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSet(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    map[string]any
		wantErr bool
	}{
		{
			name:  "simple string",
			input: "foo=bar",
			want:  map[string]any{"foo": "bar"},
		},
		{
			name:  "nested and typed values",
			input: "chart.enabled=true,chart.replicas=3,chart.ratio=1.5,chart.tags=[a,b,c]",
			want: map[string]any{
				"chart": map[string]any{
					"enabled":  true,
					"replicas": int64(3),
					"ratio":    1.5,
					"tags":     []any{"a", "b", "c"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSet(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
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

	t.Run("fileExists helper", func(t *testing.T) {
		tmpl := `{{ fileExists "/nonexistent/path/for/sure" }}`
		out, err := RenderTemplate(tmpl, nil)
		require.NoError(t, err)
		require.Equal(t, "false", out)
	})
}
