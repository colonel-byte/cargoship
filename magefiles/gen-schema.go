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
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
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

type t struct {
	T string `json:"type"`
}

type o struct {
	O []t `json:"oneOf"`
}

// Schema creates the jsonschema files for a number of the yaml files
func (Generate) Schema() error {
	var sch = []schema{
		{
			schemaStruct: &distro.ZarfDistro{},
			schemaPath:   "zarf-v1alpha1-distro-package-schema.json",
			structPath:   []string{"src", "api", "zarf.dev", "v1alpha1", "distro"},
		},
		{
			schemaStruct: &cluster.ZarfCluster{},
			schemaPath:   "zarf-v1alpha1-cluster-schema.json",
			structPath:   []string{"src", "api", "zarf.dev", "v1alpha1", "cluster"},
			keyNamer: func(s string) string {
				switch strings.ToLower(s) {
				case "openssh":
					return "openSSH"
				case "winrm":
					return "winRM"
				default:
					return strcase.LowerCamelCase(s)
				}
			},
		},
		{
			schemaStruct: &types.DistroConfig{},
			schemaPath:   "zarf-config-distro-schema.json",
			structPath:   []string{"src", "types"},
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
			return nil
		}

		// Add trailing newline to match linter expectations
		schema = append(schema, '\n')

		if err := os.WriteFile("schema/"+s.schemaPath, schema, 0644); err != nil {
			fmt.Println("Error writing schema file: ", err)
		} else {
			fmt.Println("Successfully generated " + s.schemaPath)
		}
	}
	return nil
}

// rigConfigDefName disambiguates rig v2's connection config types, which are
// all named "Config" in their own sub-packages (protocol/ssh, protocol/openssh,
// protocol/winrm). invopop/jsonschema keys $defs on the bare type name by
// default, so without this they collide into a single $defs entry -- and since
// winrm.Config embeds an *ssh.Config bastion field, reflecting any schema that
// touches more than one of them silently produces the wrong shape for whichever
// lost the collision (see winrm's "bastion" field, which would otherwise point
// at winrm.Config's own fields instead of ssh.Config's).
func rigConfigDefName(t reflect.Type) string {
	if t.Name() == "Config" {
		switch t.PkgPath() {
		case "github.com/k0sproject/rig/v2/protocol/ssh":
			return "SSHConfig"
		case "github.com/k0sproject/rig/v2/protocol/openssh":
			return "OpenSSHConfig"
		case "github.com/k0sproject/rig/v2/protocol/winrm":
			return "WinRMConfig"
		}
	}
	return ""
}

func generateV1Alpha1Schema(v any, path []string, key func(string) string) ([]byte, error) {
	reflector := jsonschema.Reflector{
		ExpandedStruct: true,
		KeyNamer:       key,
		Namer:          rigConfigDefName,
	}

	typePath := filepath.Join(path...)

	if err := reflector.AddGoComments("github.com/colonel-byte/cargoship", typePath); err != nil {
		return nil, fmt.Errorf("unable to add Go comments to schema: %w", err)
	}

	re := regexp.MustCompile(`\.([A-Za-z0-9]+)$`)

	// Strip the key from the comments
	for k, v := range reflector.CommentMap {
		matches := re.FindStringSubmatch(k)
		if len(matches) > 0 {
			reflector.CommentMap[k] = strings.TrimSpace(strings.TrimPrefix(v, matches[1]))
		}
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

	// allow the sysctl object to use numbers along side strings
	if defObj, ok := schemaMap["$defs"].(map[string]any); ok {
		if obj, ok := defObj["ZarfDistroOS"].(map[string]any); ok {
			if obj, ok := obj["properties"].(map[string]any); ok {
				if obj, ok := obj["sysctl"].(map[string]any); ok {
					obj["additionalProperties"] = o{
						O: []t{
							{
								T: "string",
							},
							{
								T: "number",
							},
						},
					}
				}
			}
		}
	}

	// rig v2's ClientWithConfig embeds a *named* CompositeConfig field tagged
	// only `yaml:",inline"` -- a yaml-only convention that invopop/jsonschema
	// (which reads json tags and only flattens true anonymous embeds) does not
	// understand. So it surfaces as a nested "connectionConfig" property instead
	// of being flattened, and rig's Client additionally promotes its embedded
	// cmd.Runner interface as a spurious top-level "runner" property. Patch the
	// generated schema to match the actual (flattened) YAML shape that rig and
	// cargoship's own config loading expect: ssh/openSSH/winRM/localhost as
	// direct ZarfHost properties, none of them required (a host sets exactly
	// one of them), matching the schema's shape before the rig v2 migration.
	if defObj, ok := schemaMap["$defs"].(map[string]any); ok {
		if zarfHost, ok := defObj["ZarfHost"].(map[string]any); ok {
			if hostProps, ok := zarfHost[propertiesKey].(map[string]any); ok {
				if compositeConfig, ok := defObj["CompositeConfig"].(map[string]any); ok {
					if ccProps, ok := compositeConfig[propertiesKey].(map[string]any); ok {
						for k, v := range ccProps {
							hostProps[k] = v
						}
					}
				}
				delete(hostProps, "connectionConfig")
				delete(hostProps, "runner")
			}
			zarfHost["required"] = []string{"role"}
			delete(defObj, "CompositeConfig")
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
