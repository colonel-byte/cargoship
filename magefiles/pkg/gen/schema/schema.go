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
	"sort"
	"strings"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/magefiles/pkg/util"
	"github.com/colonel-byte/cargoship/types"
	"github.com/invopop/jsonschema"
	"github.com/nao1215/markdown"
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

// GenerateDocs renders docs/schema/*.md from the reflected Go structs.
func GenerateDocs() error {
	for _, s := range Targets() {
		raw, err := GenerateV1Alpha1Schema(s.SchemaStruct, s.StructPath, s.namer())
		if err != nil {
			return fmt.Errorf("unable to generate %s: %w", s.SchemaPath, err)
		}

		var root map[string]any
		if err := json.Unmarshal(raw, &root); err != nil {
			return fmt.Errorf("unable to read back %s as a schema doc: %w", s.SchemaPath, err)
		}

		if err := writeSchemaDoc(s, root); err != nil {
			return err
		}
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

func writeSchemaDoc(s Target, root map[string]any) error {
	path := filepath.Join(SchemaDocsDir, s.DocFile)
	fmt.Println(path)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if err := f.Close(); err != nil {
			panic(err)
		}
	}()

	md := markdown.NewMarkdown(f)
	md.PlainText(util.GeneratedBanner)
	md.PlainText("")
	md.H2(s.DocTitle)
	md.PlainText("")
	if desc, ok := root["description"].(string); ok && desc != "" {
		md.PlainText(desc)
		md.PlainText("")
	}
	md.PlainTextf("Rendered from `schema/%s`.", s.SchemaPath)
	md.PlainText("")

	writeSchemaPropertiesTable(md, root)

	defs, _ := root["$defs"].(map[string]any)
	names := make([]string, 0, len(defs))
	for name := range defs {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		def, ok := defs[name].(map[string]any)
		if !ok {
			continue
		}
		md.H3(name)
		md.PlainText("")
		if desc, ok := def["description"].(string); ok && desc != "" {
			md.PlainText(desc)
			md.PlainText("")
		}
		writeSchemaPropertiesTable(md, def)
	}

	return md.Build()
}

func writeSchemaPropertiesTable(md *markdown.Markdown, obj map[string]any) {
	props, ok := obj[propertiesKey].(map[string]any)
	if !ok || len(props) == 0 {
		return
	}

	required := map[string]bool{}
	if req, ok := obj["required"].([]any); ok {
		for _, r := range req {
			if name, ok := r.(string); ok {
				required[name] = true
			}
		}
	}

	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)

	rows := make([][]string, 0, len(names))
	for _, name := range names {
		prop, _ := props[name].(map[string]any)

		isRequired := "no"
		if required[name] {
			isRequired = "yes"
		}

		def := ""
		if v, ok := prop["default"]; ok {
			def = "`" + util.RenderYAMLValue(v) + "`"
		}

		rows = append(rows, []string{
			"`" + name + "`",
			schemaPropertyType(prop),
			isRequired,
			def,
			schemaPropertyDescription(prop),
		})
	}

	md.PlainText(`<div class="schema-table">`)
	md.PlainText("")
	for _, line := range util.PaddedTable([]string{"Property", "Type", "Required", "Default", "Description"}, rows) {
		md.PlainText(line)
	}
	md.PlainText("")
	md.PlainText("</div>")
	md.PlainText("")
}

func schemaPropertyType(prop map[string]any) string {
	if ref, ok := prop["$ref"].(string); ok {
		name := strings.TrimPrefix(ref, "#/$defs/")
		return fmt.Sprintf("[%s](#%s)", name, strings.ToLower(name))
	}

	t, _ := prop["type"].(string)
	switch t {
	case "array":
		items, _ := prop["items"].(map[string]any)
		return "array of " + schemaPropertyType(items)
	case "object":
		if ap, ok := prop["additionalProperties"].(map[string]any); ok {
			return "map[string]" + schemaPropertyType(ap)
		}
		return "object"
	case "":
		if alt, ok := prop["oneOf"].([]any); ok {
			return schemaPropertyAlternatives(alt)
		}
		if alt, ok := prop["anyOf"].([]any); ok {
			return schemaPropertyAlternatives(alt)
		}
		return "any"
	default:
		return "`" + t + "`"
	}
}

func schemaPropertyAlternatives(alternatives []any) string {
	types := make([]string, 0, len(alternatives))
	for _, a := range alternatives {
		if branch, ok := a.(map[string]any); ok {
			types = append(types, schemaPropertyType(branch))
		}
	}
	return strings.Join(types, " or ")
}

func schemaPropertyDescription(prop map[string]any) string {
	desc, _ := prop["description"].(string)

	if desc == "" {
		_, hasType := prop["type"]
		_, hasRef := prop["$ref"]
		_, hasOneOf := prop["oneOf"]
		_, hasAnyOf := prop["anyOf"]
		if !hasType && !hasRef && !hasOneOf && !hasAnyOf {
			desc = "Accepts any value; cargoship does not constrain its shape here."
		}
	}

	enum, ok := prop["enum"].([]any)
	if !ok || len(enum) == 0 {
		return desc
	}

	quoted := make([]string, 0, len(enum))
	for _, e := range enum {
		quoted = append(quoted, fmt.Sprintf("`%v`", e))
	}
	note := fmt.Sprintf("One of %s.", strings.Join(quoted, ", "))
	if desc == "" {
		return note
	}
	return desc + " " + note
}
