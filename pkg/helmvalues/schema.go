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
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/xeipuuv/gojsonschema"
)

// Schema is a parsed JSON schema document describing what a package's values
// may hold.
type Schema map[string]any

// refKeywords are the JSON Schema keywords that name another document. A schema
// travels inside a signed package, so resolving one of these would let a
// validated package reach out to a host the operator never agreed to contact,
// and would make validation fail closed on an air-gapped network - the one
// cargoship is built for. They are rejected when the schema is loaded.
var refKeywords = []string{"$ref", "$dynamicRef", "$recursiveRef"}

// ParseSchema decodes a JSON schema document. name is used in error messages.
//
// The schema is checked for external references and compiled here rather than
// at validation time, so a malformed schema fails when the package is built
// rather than when it is deployed.
func ParseSchema(name string, b []byte) (Schema, error) {
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("parsing values schema %s: %w", name, err)
	}
	if doc == nil {
		return nil, fmt.Errorf("values schema %s is empty", name)
	}
	if err := checkNoExternalRefs(doc, name); err != nil {
		return nil, err
	}
	if _, err := gojsonschema.NewSchemaLoader().Compile(gojsonschema.NewGoLoader(doc)); err != nil {
		return nil, fmt.Errorf("invalid values schema %s: %w", name, err)
	}
	return doc, nil
}

// LoadSchema reads and parses a JSON schema file.
func LoadSchema(path string) (Schema, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading values schema: %w", err)
	}
	return ParseSchema(path, b)
}

// checkNoExternalRefs walks a schema document and rejects any reference
// keyword. Local references ("#/$defs/x") are rejected along with remote ones:
// allowing them would mean re-walking the resolved target to prove it does not
// itself reach outside, and a values schema is small enough to write flat.
func checkNoExternalRefs(node any, name string) error {
	switch n := node.(type) {
	case map[string]any:
		for _, kw := range refKeywords {
			if _, ok := n[kw]; ok {
				return fmt.Errorf("values schema %s uses %q, which is not supported: write the schema without references", name, kw)
			}
		}
		for _, v := range n {
			if err := checkNoExternalRefs(v, name); err != nil {
				return err
			}
		}
	case []any:
		for _, v := range n {
			if err := checkNoExternalRefs(v, name); err != nil {
				return err
			}
		}
	}
	return nil
}

// Validate checks values against the schema and returns a ValidationError
// listing every problem found, not just the first. A nil or empty schema
// validates anything, so a package without a schema is not forced to have one.
func (s Schema) Validate(values map[string]any) error {
	if len(s) == 0 {
		return nil
	}
	if values == nil {
		// gojsonschema rejects a nil document outright; an absent set of values
		// is an empty one, and the schema decides whether that is allowed.
		values = map[string]any{}
	}

	result, err := gojsonschema.Validate(gojsonschema.NewGoLoader(map[string]any(s)), gojsonschema.NewGoLoader(values))
	if err != nil {
		return fmt.Errorf("validating values: %w", err)
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
		problems = append(problems, fmt.Sprintf("%s: %s", where, e.Description()))
	}
	// gojsonschema does not promise an order, and a build log that reorders
	// between runs is hard to diff.
	sort.Strings(problems)
	return &ValidationError{Problems: problems}
}

// ValidationError reports every way a set of values failed its schema.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("values do not match the schema:\n  %s", strings.Join(e.Problems, "\n  "))
}
