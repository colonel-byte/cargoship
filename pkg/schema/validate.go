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

package schema

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	goyaml "github.com/goccy/go-yaml"
	"github.com/xeipuuv/gojsonschema"
)

// documentKinds maps the kind field a document declares onto the schema that describes it. A
// cargoship config file has no kind field, which is why it is absent here and has to be named
// explicitly.
var documentKinds = map[string]Kind{
	"ZarfCluster": KindInventory,
	"ZarfDistro":  KindPackage,
}

// Decode reads a YAML document into the shape a JSON Schema validator expects.
//
// The round trip through JSON is not redundant. YAML carries types JSON Schema has no words for
// -- a timestamp, an integer key, a value larger than a float64 -- and a validator handed one of
// them reports a type error that has nothing to do with the file. Encoding to JSON and back
// settles every value into the six types the schema is written against, which is the same
// normalization the document would undergo on its way to any other JSON Schema tool.
func Decode(b []byte) (any, error) {
	var doc any
	if err := goyaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("unable to parse YAML: %w", err)
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("unable to convert the document to JSON: %w", err)
	}
	var out any
	if err := json.Unmarshal(encoded, &out); err != nil {
		return nil, fmt.Errorf("unable to convert the document to JSON: %w", err)
	}
	return out, nil
}

// DetectKind reads the schema kind out of a decoded document's own kind field, so that a file
// says what it is rather than the operator having to.
func DetectKind(doc any) (Kind, error) {
	obj, ok := doc.(map[string]any)
	if !ok {
		return "", fmt.Errorf("document is not a YAML mapping, so it declares no kind")
	}
	declared, ok := obj["kind"].(string)
	if !ok || declared == "" {
		return "", fmt.Errorf("document declares no kind: name the schema to check it against")
	}
	kind, ok := documentKinds[declared]
	if !ok {
		return "", fmt.Errorf("document declares kind %q, which cargoship has no schema for", declared)
	}
	return kind, nil
}

// Validator checks documents against one schema. The schema is compiled once and reused, which is
// what makes checking a directory of packages worth doing in a single process.
type Validator struct {
	kind   Kind
	schema *gojsonschema.Schema
}

// NewValidator compiles a schema document for repeated use. Pass the document rather than the
// kind, so that a composed inventory schema is checked exactly as an editor would check it.
func NewValidator(kind Kind, doc map[string]any) (*Validator, error) {
	compiled, err := gojsonschema.NewSchemaLoader().Compile(gojsonschema.NewGoLoader(doc))
	if err != nil {
		return nil, fmt.Errorf("unable to compile the %s schema: %w", kind, err)
	}
	return &Validator{kind: kind, schema: compiled}, nil
}

// NewValidatorFor compiles the generated schema for a kind, for callers with nothing to compose.
func NewValidatorFor(kind Kind) (*Validator, error) {
	doc, err := Load(kind)
	if err != nil {
		return nil, err
	}
	return NewValidator(kind, doc)
}

// Kind returns the schema this validator checks against.
func (v *Validator) Kind() Kind {
	return v.kind
}

// Validate checks a decoded document and returns a ValidationError listing every problem, not just
// the first: a file with four mistakes in it should take one run to fix, not four.
func (v *Validator) Validate(source string, doc any) error {
	result, err := v.schema.Validate(gojsonschema.NewGoLoader(doc))
	if err != nil {
		return fmt.Errorf("unable to check %s: %w", source, err)
	}
	if result.Valid() {
		return nil
	}

	problems := make([]string, 0, len(result.Errors()))
	for _, e := range result.Errors() {
		where := e.Field()
		if where == gojsonschema.STRING_CONTEXT_ROOT {
			where = "(root)"
		}
		problems = append(problems, fmt.Sprintf("%s: %s", where, describe(where, e.Description())))
	}
	// gojsonschema does not promise an order, and output that reorders between runs is hard to
	// diff and hard to assert on.
	sort.Strings(problems)

	return &ValidationError{Source: source, Kind: v.kind, Problems: problems}
}

// describe trims the field path gojsonschema repeats at the front of some of its messages -- an
// enum failure reads "spec.hosts.0.role must be one of ..." -- so that every line comes out as one
// field followed by one problem rather than naming the field twice.
func describe(where, description string) string {
	trimmed := strings.TrimPrefix(description, where+" ")
	if trimmed == "" {
		return description
	}
	return trimmed
}

// ValidationError reports every way one document failed its schema.
type ValidationError struct {
	Source   string
	Kind     Kind
	Problems []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s does not match the %s schema:\n  %s", e.Source, e.Kind, strings.Join(e.Problems, "\n  "))
}
