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
	"strings"

	cluster "github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	distro "github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/types"
	"github.com/invopop/jsonschema"
	strcase "github.com/stoewer/go-strcase"
)

const (
	propertiesKey        = "properties"
	patternPropertiesKey = "patternProperties"
	yamlExtensionRegex   = "^x-"
)

type schema struct {
	schemaStruct any
	schemaPath   string
	structPath   []string
	keyNamer     func(string) string
}

func main() {
	var sch = []schema{
		{
			schemaStruct: &distro.ZarfDistro{},
			schemaPath:   "zarf-v1alpha1-distro-package-schema.json",
			structPath:   []string{"src", "types"},
		},
		{
			schemaStruct: &cluster.ZarfCluster{},
			schemaPath:   "zarf-v1alpha1-cluster-schema.json",
			structPath:   []string{"src", "api", "zarf.dev", "v1alpha1", "cluster"},
			keyNamer: func(s string) string {
				if strings.ToLower(s) == "openssh" {
					return "openSSH"
				}
				return strcase.LowerCamelCase(s)
			},
		},
		{
			schemaStruct: &types.DistroConfig{},
			schemaPath:   "zarf-config-distro-schema.json",
			structPath:   []string{"src", "api", "zarf.dev", "v1alpha1", "distro"},
			keyNamer: func(s string) string {
				return s
			},
		},
	}

	for _, s := range sch {
		var schema []byte
		var err error

		if s.keyNamer != nil {
			schema, err = generateV1Alpha1Schema(s.schemaStruct, s.structPath, s.keyNamer)
		} else {
			schema, err = generateV1Alpha1Schema(s.schemaStruct, s.structPath, strcase.LowerCamelCase)
		}

		if err != nil {
			fmt.Println("Error generating schema: ", err)
			os.Exit(1)
		}

		// Add trailing newline to match linter expectations
		schema = append(schema, '\n')

		if err := os.WriteFile("schema/"+s.schemaPath, schema, 0644); err != nil {
			fmt.Println("Error writing schema file: ", err)
			os.Exit(1)
		}

		fmt.Println("Successfully generated " + s.schemaPath)
	}
}

func generateV1Alpha1Schema(v any, path []string, key func(string) string) ([]byte, error) {
	reflector := jsonschema.Reflector{
		ExpandedStruct: true,
		IgnoredTypes:   []any{},
		KeyNamer:       key,
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

	typePath := filepath.Join(append([]string{"..", "..", ".."}, path...)...)

	if err := reflector.AddGoComments("github.com/colonel-byte/cargoship", typePath); err != nil {
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
		if sshObj, ok := defObj["WinRM"].(map[string]any); ok {
			sshObj["required"] = []string{
				"address",
				"user",
				"port",
			}
		}
		if sshObj, ok := defObj["ZarfHost"].(map[string]any); ok {
			sshObj["required"] = []string{
				"role",
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
