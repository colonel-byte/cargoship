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

// The five below are what Zarf's action templating has and sprout's sprig
// compatibility layer does not. Cargoship renders actions itself, so an action
// that uses one has to keep rendering the same way after the move.
func TestHelmExtrasAreAvailable(t *testing.T) {
	for _, name := range []string{"toToml", "fromToml", "toYamlPretty", "fromYamlArray", "fromJsonArray"} {
		require.Contains(t, FuncMap(), name)
	}
}

func TestHelmExtrasRender(t *testing.T) {
	data := map[string]any{
		"Values": map[string]any{
			"registry": map[string]any{"address": "127.0.0.1:31999"},
			"yamlList": "- one\n- two\n",
			"jsonList": `["one","two"]`,
			"toml":     "address = \"127.0.0.1:31999\"\n",
		},
	}

	for _, tt := range []struct {
		name string
		tmpl string
		want string
	}{
		{"toToml", `{{ toToml .Values.registry }}`, "address = \"127.0.0.1:31999\"\n"},
		{"fromToml", `{{ (fromToml .Values.toml).address }}`, "127.0.0.1:31999"},
		{"toYamlPretty", `{{ toYamlPretty .Values.registry }}`, "address: 127.0.0.1:31999"},
		{"fromYamlArray", `{{ index (fromYamlArray .Values.yamlList) 1 }}`, "two"},
		{"fromJsonArray", `{{ index (fromJsonArray .Values.jsonList) 0 }}`, "one"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RenderTemplate(tt.tmpl, data)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// Helm reports a parse failure through the data rather than by failing the
// render, and these follow it, so a template that takes the error branch still
// produces output a reader can see.
func TestHelmExtrasReportParseFailuresInBand(t *testing.T) {
	got, err := RenderTemplate(`{{ (fromToml "= nope").Error }}`, nil)
	require.NoError(t, err)
	require.NotEmpty(t, got)

	got, err = RenderTemplate(`{{ index (fromJsonArray "not json") 0 }}`, nil)
	require.NoError(t, err)
	require.NotEmpty(t, got)
}
