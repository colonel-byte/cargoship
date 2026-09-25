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

	"github.com/colonel-byte/cargoship/src/pkg/helmvalues"
	"github.com/stretchr/testify/require"
)

// templateFuzzData is the render context every template fuzz target renders against: a package's
// own values nested under .Values, the way Helm and Zarf actions see them, plus a couple of the
// well-known top-level keys (.Chart, .Release) a template copied out of a chart expects to exist.
func templateFuzzData() map[string]any {
	return map[string]any{
		"Values": map[string]any{
			"enabled":  true,
			"name":     "fuzz",
			"replicas": 3,
			"nested":   map[string]any{"inner": "value"},
			"list":     []any{"a", "b", "c"},
		},
		"Chart":   map[string]any{"Name": "fuzz-chart", "Version": "1.0.0"},
		"Release": map[string]any{"Name": "fuzz-release", "Namespace": "fuzz-ns"},
	}
}

// FuzzRenderTemplateNoPanic asserts that RenderTemplate, with the default function map every
// values and manifest template renders under, never panics whatever template text a package
// author writes.
//
// The default map is sprig's whole surface minus deniedFuncs, none of it under WithCreateFuncs, so
// this exercises the functions cargoship trusts a package to call unconditionally -- string and
// list helpers, the serialization helpers in helmExtras, base64 and regex functions -- against
// inputs a hand-written template would not think to try: a negative repeat count, a malformed
// regex, an index past the end of a fuzzed list. A panic here takes down whatever command is
// building or applying the package with it, where a returned error would just fail that one
// package.
func FuzzRenderTemplateNoPanic(f *testing.F) {
	f.Add("{{ .Values.name }}")
	f.Add("{{ .Values.missing }}")
	f.Add("{{ .Values.nested.inner }}")
	f.Add("{{ .Values.list | join \",\" }}")
	f.Add("{{ .Values.name | upper | lower | title }}")
	f.Add("{{ .Values.name | repeat -1 }}")
	f.Add("{{ .Values.name | trunc -5 }}")
	f.Add("{{ regexMatch \"[\" .Values.name }}")
	f.Add("{{ .Values.replicas | int | add 1 }}")
	f.Add("{{ toYaml .Values }}")
	f.Add("{{ .Values | toToml }}")
	f.Add("{{ fromYamlArray .Values.name }}")
	f.Add("{{ fromJsonArray .Values.name }}")
	f.Add("{{ range .Values.list }}{{ . }}{{ end }}")
	f.Add("{{ if .Values.enabled }}on{{ else }}off{{ end }}")
	f.Add("{{ .Values.list | first | last }}")
	f.Add("{{ env \"HOME\" }}")
	f.Add("{{ .Values.name.doesnotexist.either }}")
	f.Add("{{ printf \"%d\" .Values.name }}")
	f.Add("{{")
	f.Add("{{ . }}")
	f.Add("{{ .Values.list | slice 100 200 }}")
	f.Add("plain text, no template at all")
	f.Add("")

	f.Fuzz(func(t *testing.T, tmpl string) {
		_, _ = helmvalues.RenderTemplate(tmpl, templateFuzzData())
	})
}

// FuzzRenderTemplateDeniesHostState asserts that a template calling one of the functions
// cargoship removes for reading host state is always rejected -- as a template parse error,
// never as a render that silently reads the operator's environment or DNS into a chart value.
//
// deniedFuncs is a denylist maintained by hand against sprout's own function set, which is exactly
// the kind of list a dependency upgrade can quietly grow around: sprout adding an alias for env or
// expandenv under a new name would leave this fuzz target's calls to that alias succeeding instead
// of failing to parse, which is the regression worth catching.
func FuzzRenderTemplateDeniesHostState(f *testing.F) {
	deniedCalls := []string{
		"{{ env \"HOME\" }}",
		"{{ expandEnv .Values.name }}",
		"{{ expandenv .Values.name }}",
		"{{ getHostByName \"localhost\" }}",
	}

	for _, call := range deniedCalls {
		f.Add(call)
	}

	f.Fuzz(func(t *testing.T, prefix string) {
		for _, call := range deniedCalls {
			_, err := helmvalues.RenderTemplate(prefix+call, templateFuzzData())
			require.Error(t, err, "%q rendered instead of being rejected as an unknown function", call)
		}
	})
}

// FuzzEvaluateValuesTemplatesNoPanic asserts that EvaluateValuesTemplates never panics walking a
// values document decoded from arbitrary YAML, whatever shape of map, list and scalar the decoder
// hands it.
//
// EvaluateValuesTemplates recurses over map[string]any, dig.Mapping, map[any]any and []any, typing
// the result of each rendered string afterwards. The shape-handling and the typing are both places
// a case can be missed -- a map[any]any key that is not a string, an empty list, a string that
// renders to something typedScalar does not expect -- and this is the one target that drives them
// with real YAML-shaped structures rather than a value built by hand.
func FuzzEvaluateValuesTemplatesNoPanic(f *testing.F) {
	f.Add([]byte(`a: "{{ .Values.name }}"`))
	f.Add([]byte("a:\n  b: \"{{ .Values.nested.inner }}\"\n"))
	f.Add([]byte("a: [\"{{ .Values.name }}\", \"{{ .Values.missing }}\"]\n"))
	f.Add([]byte("1: \"{{ .Values.name }}\"\ntrue: 2\n"))
	f.Add([]byte("a: \"{{\"\n"))
	f.Add([]byte("a: \"plain, no template\"\n"))
	f.Add([]byte("a: 1\nb: true\nc: null\n"))
	f.Add([]byte(""))

	f.Fuzz(func(t *testing.T, doc []byte) {
		values, ok := decodeValuesMap(t, doc)
		if !ok {
			return
		}
		_, _ = helmvalues.EvaluateValuesTemplates(values, templateFuzzData())
	})
}
