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

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unicode"
	"unicode/utf8"

	"github.com/colonel-byte/zarf-distro/src/api/v1alpha1"
	"github.com/invopop/jsonschema"
)

const (
	propertiesKey        = "properties"
	patternPropertiesKey = "patternProperties"
	yamlExtensionRegex   = "^x-"
)

type schema struct {
	schemaStruct any
	path         string
}

func main() {
	var sch = []schema{
		{
			schemaStruct: &v1alpha1.ZarfDistroPackage{},
			path:         "zarf-distro-package-v1alpha1-schema.json",
		},
		{
			schemaStruct: &v1alpha1.ZarfDistroInstall{},
			path:         "zarf-distro-install-v1alpha1-schema.json",
		},
	}

	for _, s := range sch {
		schema, err := generateV1Alpha1Schema(s.schemaStruct)
		if err != nil {
			fmt.Println("Error generating schema: ", err)
			os.Exit(1)
		}

		// Add trailing newline to match linter expectations
		schema = append(schema, '\n')

		if err := os.WriteFile("schema/"+s.path, schema, 0644); err != nil {
			fmt.Println("Error writing schema file: ", err)
			os.Exit(1)
		}

		fmt.Println("Successfully generated " + s.path)
	}
}

func generateV1Alpha1Schema(v any) ([]byte, error) {
	reflector := jsonschema.Reflector{
		ExpandedStruct: true,
		IgnoredTypes:   []any{},
	}

	// AddGoComments breaks if called with an absolute path, so we save the current
	// directory, move to the directory of this source file, then use a relative path
	originalDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("unable to get current directory: %w", err)
	}
	defer os.Chdir(originalDir)

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return nil, errors.New("unable to get the current filename")
	}
	schemaDir := filepath.Dir(filename)
	if err := os.Chdir(schemaDir); err != nil {
		return nil, fmt.Errorf("unable to change to schema directory: %w", err)
	}

	typePackagePath := filepath.Join("..", "..", "api", "v1alpha1")

	// Get the Go comments from the v1alpha1 package
	if err := reflector.AddGoComments("github.com/colonel-byte/zarf-distro/src/api/v1alpha1", typePackagePath); err != nil {
		return nil, fmt.Errorf("unable to add Go comments to schema: %w", err)
	}

	schema := reflector.Reflect(v)

	schemaData, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("unable to marshal schema: %w", err)
	}

	var schemaMap map[string]any
	if err := json.Unmarshal(schemaData, &schemaMap); err != nil {
		return nil, fmt.Errorf("unable to unmarshal schema: %w", err)
	}

	addYAMLExtensions(schemaMap)

	// clean up the rig.OpenSSH properties for schema
	if defObj, ok := schemaMap["$defs"].(map[string]any); ok {
		if sshObj, ok := defObj["OpenSSH"].(map[string]any); ok {
			sshObj["required"] = []string{
				"address",
			}
		}
	}

	output, err := json.MarshalIndent(schemaMap, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("unable to marshal final schema: %w", err)
	}

	return output, nil
}

// addYAMLExtensions walks through the JSON schema and adds patternProperties
// for "x-" prefixed fields to any object that has "properties".
// This allows YAML extensions (custom fields starting with x-) to be valid.
func addYAMLExtensions(data map[string]any) {
	if _, hasProperties := data[propertiesKey]; hasProperties {
		if _, hasPatternProps := data[patternPropertiesKey]; !hasPatternProps {
			data[patternPropertiesKey] = map[string]any{
				yamlExtensionRegex: map[string]any{},
			}
		}
		if obj, ok := data[propertiesKey].(map[string]any); ok {
			for k := range obj {
				if IsFirstCharUpper(k) {
					obj[FirstToLower(k)] = obj[k]
					delete(obj, k)
				}
			}
		}
	}

	for _, v := range data {
		switch val := v.(type) {
		case map[string]any:
			addYAMLExtensions(val)
		case []any:
			for _, item := range val {
				if obj, ok := item.(map[string]any); ok {
					addYAMLExtensions(obj)
				}
			}
		}
	}
}

func IsFirstCharUpper(s string) bool {
	if s == "" {
		return false
	}
	runes := []rune(s)
	return unicode.IsUpper(runes[0])
}

func FirstToLower(s string) string {
	if s == "" {
		return s
	}

	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}

	lc := unicode.ToLower(r)
	if r == lc {
		return s
	}

	return string(lc) + s[size:]
}
