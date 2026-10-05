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
	"bytes"
	"encoding/json"
	"strings"
	"text/template"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// helmExtras are the serialization helpers Helm defines in its own engine and
// Zarf carries in its action templating, which sprout's sprig compatibility
// layer does not provide. They are added so a package author sees one function
// set: everything sprig offers, everything Zarf's actions offer, and, for
// onCreate actions only, the create helpers from addCreateFuncs.
//
// The five below follow Helm's convention of reporting a failure through the
// returned data - "Error" for a map, a single-element slice for a list, the
// message itself for a string - rather than by failing the render. That reads
// backwards for cargoship, which would rather stop, but matching Helm and Zarf
// matters more here: these are the semantics a template copied out of a chart
// was written against, and a template that silently takes the "Error" branch is
// still visible in the rendered output.
func helmExtras() template.FuncMap {
	return template.FuncMap{
		"toToml":        toTOML,
		"fromToml":      fromTOML,
		"toYamlPretty":  toYAMLPretty,
		"fromYamlArray": fromYAMLArray,
		"fromJsonArray": fromJSONArray,
	}
}

// toTOML marshals v to TOML, returning the error text on failure.
func toTOML(v any) string {
	b := bytes.NewBuffer(nil)
	if err := toml.NewEncoder(b).Encode(v); err != nil {
		return err.Error()
	}
	return b.String()
}

// fromTOML unmarshals a TOML document into a map, reporting a failure under the
// "Error" key.
func fromTOML(str string) map[string]any {
	m := map[string]any{}
	if err := toml.Unmarshal([]byte(str), &m); err != nil {
		m["Error"] = err.Error()
	}
	return m
}

// toYAMLPretty marshals v to YAML indented by two spaces, without the trailing
// newline an encoder writes, so the result drops into a template where it is
// called rather than pushing what follows onto its own line.
func toYAMLPretty(v any) string {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(v); err != nil {
		return ""
	}
	if err := encoder.Close(); err != nil {
		return ""
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

// fromYAMLArray unmarshals a YAML sequence, reporting a failure as a one-element
// slice holding the error text.
func fromYAMLArray(str string) []any {
	a := []any{}
	if err := yaml.Unmarshal([]byte(str), &a); err != nil {
		a = []any{err.Error()}
	}
	return a
}

// fromJSONArray unmarshals a JSON array, reporting a failure as a one-element
// slice holding the error text.
func fromJSONArray(str string) []any {
	a := []any{}
	if err := json.Unmarshal([]byte(str), &a); err != nil {
		a = []any{err.Error()}
	}
	return a
}
