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

// Package schema generates and renders JSON schemas from Go API types.
package schema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/types"
	"github.com/invopop/jsonschema"
	strcase "github.com/stoewer/go-strcase"
)

const (
	propertiesKey        = "properties"
	patternPropertiesKey = "patternProperties"
	yamlExtensionRegex   = "^x-"
)

var (
	SchemaDir         = "schema"
	SchemaEmbedDir    = filepath.Join("pkg", "schema", "embedded")
	SchemaDocsDir     = "docs/schema"
	sharedV1Alpha1Dir = filepath.Join("api", "zarf.dev", "v1alpha1")
)

// Target describes a schema generation target.
type Target struct {
	SchemaStruct any
	SchemaPath   string
	StructPath   []string
	KeyNamer     func(string) string
	DocTitle     string
	DocFile      string
}

func (s Target) namer() func(string) string {
	if s.KeyNamer != nil {
		return s.KeyNamer
	}
	return strcase.LowerCamelCase
}

// Targets returns the canonical list of structs published as JSON schemas.
func Targets() []Target {
	return []Target{
		{
			SchemaStruct: &distro.ZarfDistro{},
			SchemaPath:   "zarf-v1alpha1-distro-package-schema.json",
			StructPath:   []string{"api", "zarf.dev", "v1alpha1", "distro"},
			DocTitle:     "Distro package",
			DocFile:      "distro.md",
		},
		{
			SchemaStruct: &cluster.ZarfCluster{},
			SchemaPath:   "zarf-v1alpha1-cluster-schema.json",
			StructPath:   []string{"api", "zarf.dev", "v1alpha1", "cluster"},
			KeyNamer: func(s string) string {
				switch strings.ToLower(s) {
				case "openssh":
					return "openSSH"
				case "winrm":
					return "winRM"
				default:
					return strcase.LowerCamelCase(s)
				}
			},
			DocTitle: "Cluster configuration",
			DocFile:  "cluster.md",
		},
		{
			SchemaStruct: &types.DistroConfig{},
			SchemaPath:   "zarf-config-distro-schema.json",
			StructPath:   []string{"types"},
			KeyNamer: func(s string) string {
				return s
			},
			DocTitle: "Cargoship configuration file",
			DocFile:  "config.md",
		},
	}
}

// GenerateSchemas creates the jsonschema files under schema/ and pkg/schema/embedded/.
func GenerateSchemas() error {
	for _, s := range Targets() {
		b, err := GenerateV1Alpha1Schema(s.SchemaStruct, s.StructPath, s.namer())
		if err != nil {
			return fmt.Errorf("unable to generate %s: %w", s.SchemaPath, err)
		}
		b = append(b, '\n')

		for _, dir := range []string{SchemaDir, SchemaEmbedDir} {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("unable to create %s: %w", dir, err)
			}
			if err := os.WriteFile(filepath.Join(dir, s.SchemaPath), b, 0644); err != nil {
				return fmt.Errorf("unable to write %s: %w", filepath.Join(dir, s.SchemaPath), err)
			}
		}
		fmt.Println("Successfully generated " + s.SchemaPath)
	}
	return nil
}
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

// GenerateV1Alpha1Schema reflects a struct into JSON Schema bytes.
func GenerateV1Alpha1Schema(v any, path []string, key func(string) string) ([]byte, error) {
	reflector := jsonschema.Reflector{
		ExpandedStruct: true,
		KeyNamer:       key,
		Namer:          rigConfigDefName,
	}

	typePath := filepath.Join(path...)

	if err := reflector.AddGoComments("github.com/colonel-byte/cargoship", typePath); err != nil {
		return nil, fmt.Errorf("unable to add Go comments to schema: %w", err)
	}

	if typePath != sharedV1Alpha1Dir {
		if err := reflector.AddGoComments("github.com/colonel-byte/cargoship", sharedV1Alpha1Dir); err != nil {
			return nil, fmt.Errorf("unable to add Go comments to schema: %w", err)
		}
	}

	re := regexp.MustCompile(`\.([A-Za-z0-9]+)$`)

	for k, v := range reflector.CommentMap {
		matches := re.FindStringSubmatch(k)
		if len(matches) > 0 {
			reflector.CommentMap[k] = strings.TrimSpace(strings.TrimPrefix(v, matches[1]))
		}
	}

	s := reflector.Reflect(v)

	schemaData, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("unable to marshal schema: %w", err)
	}

	var schemaMap map[string]any
	if err := json.Unmarshal(schemaData, &schemaMap); err != nil {
		return nil, fmt.Errorf("unable to unmarshal schema: %w", err)
	}

	addYAMLExtensions(schemaMap)

	if defObj, ok := schemaMap["$defs"].(map[string]any); ok {
		if zarfHost, ok := defObj["ZarfHost"].(map[string]any); ok {
			if hostProps, ok := zarfHost[propertiesKey].(map[string]any); ok {
				if compositeConfig, ok := defObj["CompositeConfig"].(map[string]any); ok {
					if ccProps, ok := compositeConfig[propertiesKey].(map[string]any); ok {
						for k, val := range ccProps {
							hostProps[k] = val
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
