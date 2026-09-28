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

// Package schema reflects the Go types this repository publishes JSON schemas for, and writes
// schema/*.json from them.
//
// The reflection is exported rather than kept private because gen/schemadoc renders docs/schema
// from the same schemas, in memory, rather than from the files on disk -- so the page and the
// schema can never disagree.
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

// PropertiesKey is the JSON schema keyword holding an object's fields. Exported because
// gen/schemadoc walks the same documents.
const PropertiesKey = "properties"

// PatternPropertiesKey is the JSON schema keyword holding the keys an object accepts by pattern,
// which is how addYAMLExtensions admits x- extensions.
const PatternPropertiesKey = "patternProperties"

const yamlExtensionRegex = "^x-"

// Dir is the canonical location every published distro.yaml and the inventory guide
// reference by raw.githubusercontent.com URL. It cannot move.
var Dir = "schema"

// EmbedDir holds the copy `cargoship schema` serves out of the binary. go:embed cannot
// reach outside its own package directory, so the copy lives next to the Go file that embeds
// it rather than being read from Dir. Both are written by the same target, and
// pre-commit runs that target, so the two cannot drift.
var EmbedDir = filepath.Join("pkg", "schema", "embedded")

// sharedV1Alpha1Dir holds the ZarfFile and BinarySelector types the distro and cluster APIs both
// embed. Reflect walks it in addition to a schema's own StructPath, so their doc comments reach
// the schema even though neither of those two directories is an ancestor of it.
var sharedV1Alpha1Dir = filepath.Join("api", "zarf.dev", "v1alpha1")

// Target is one struct this repository publishes a JSON schema for.
type Target struct {
	Struct     any
	Path       string
	StructPath []string
	KeyNamer   func(string) string

	// DocTitle and DocFile name this schema's page under docs/schema/, rendered by gen/schemadoc.
	// They are independent of Path, which is fixed by the published raw.githubusercontent.com URLs
	// Dir cannot move.
	DocTitle string
	DocFile  string
}

// Namer is the KeyNamer Reflect reflects the struct with: the target's own, or
// strcase.LowerCamelCase when it declares none.
func (t Target) Namer() func(string) string {
	if t.KeyNamer != nil {
		return t.KeyNamer
	}
	return strcase.LowerCamelCase
}

// Targets lists every struct this repository publishes a JSON schema for, shared by
// Generate, which writes schema/*.json, and gen/schemadoc, which renders docs/schema/ from the
// same reflection.
func Targets() []Target {
	return []Target{
		{
			Struct:     &distro.ZarfDistro{},
			Path:       "zarf-v1alpha1-distro-package-schema.json",
			StructPath: []string{"api", "zarf.dev", "v1alpha1", "distro"},
			DocTitle:   "Distro package",
			DocFile:    "distro.md",
		},
		{
			Struct:     &cluster.ZarfCluster{},
			Path:       "zarf-v1alpha1-cluster-schema.json",
			StructPath: []string{"api", "zarf.dev", "v1alpha1", "cluster"},
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
			Struct:     &types.DistroConfig{},
			Path:       "zarf-config-distro-schema.json",
			StructPath: []string{"types"},
			KeyNamer: func(s string) string {
				return s
			},
			DocTitle: "Cargoship configuration file",
			DocFile:  "config.md",
		},
	}
}

// Generate writes every target's schema to both Dir and EmbedDir.
func Generate() error {
	for _, t := range Targets() {
		schema, err := Reflect(t.Struct, t.StructPath, t.Namer())
		if err != nil {
			return fmt.Errorf("unable to generate %s: %w", t.Path, err)
		}

		// Add trailing newline to match linter expectations
		schema = append(schema, '\n')

		for _, dir := range []string{Dir, EmbedDir} {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("unable to create %s: %w", dir, err)
			}
			// A silently failed write here would ship a stale schema inside the binary,
			// so this reports rather than continuing.
			if err := os.WriteFile(filepath.Join(dir, t.Path), schema, 0644); err != nil {
				return fmt.Errorf("unable to write %s: %w", filepath.Join(dir, t.Path), err)
			}
		}
		fmt.Println("Successfully generated " + t.Path)
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

// Reflect renders one struct's JSON schema, as the indented bytes written to disk and read back
// by gen/schemadoc.
func Reflect(v any, path []string, key func(string) string) ([]byte, error) {
	reflector := jsonschema.Reflector{
		ExpandedStruct: true,
		KeyNamer:       key,
		Namer:          rigConfigDefName,
	}

	typePath := filepath.Join(path...)

	if err := reflector.AddGoComments("github.com/colonel-byte/cargoship", typePath); err != nil {
		return nil, fmt.Errorf("unable to add Go comments to schema: %w", err)
	}

	// AddGoComments only walks typePath itself, so a struct's own comments never reach fields whose
	// type -- ZarfFile, BinarySelector -- is defined one directory up, in the shared v1alpha1
	// package both distro and cluster embed. Without a comment, invopop/jsonschema collapses an
	// "any" field's schema to the bare boolean `true`, which cannot carry a description at all (see
	// PermMode in api/zarf.dev/v1alpha1/file.go). Walking the shared package too fixes that for
	// every schema, not just the ones that happen to reference it.
	if typePath != sharedV1Alpha1Dir {
		if err := reflector.AddGoComments("github.com/colonel-byte/cargoship", sharedV1Alpha1Dir); err != nil {
			return nil, fmt.Errorf("unable to add Go comments to schema: %w", err)
		}
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
	flattenRigHostConfig(schemaMap)

	output, err := json.MarshalIndent(schemaMap, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("unable to marshal final schema: %w", err)
	}

	return output, nil
}

// flattenRigHostConfig patches ZarfHost into the shape its YAML actually has.
//
// rig v2's ClientWithConfig embeds a *named* CompositeConfig field tagged only `yaml:",inline"` --
// a yaml-only convention that invopop/jsonschema (which reads json tags and only flattens true
// anonymous embeds) does not understand. So it surfaces as a nested "connectionConfig" property
// instead of being flattened, and rig's Client additionally promotes its embedded cmd.Runner
// interface as a spurious top-level "runner" property. The result here matches the actual
// (flattened) YAML shape that rig and cargoship's own config loading expect: ssh/openSSH/winRM/
// localhost as direct ZarfHost properties, none of them required (a host sets exactly one of
// them), matching the schema's shape before the rig v2 migration.
func flattenRigHostConfig(schemaMap map[string]any) {
	defObj, ok := schemaMap["$defs"].(map[string]any)
	if !ok {
		return
	}
	zarfHost, ok := defObj["ZarfHost"].(map[string]any)
	if !ok {
		return
	}
	if hostProps, ok := zarfHost[PropertiesKey].(map[string]any); ok {
		if compositeConfig, ok := defObj["CompositeConfig"].(map[string]any); ok {
			if ccProps, ok := compositeConfig[PropertiesKey].(map[string]any); ok {
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

// addYAMLExtensions walks through the JSON schema and adds patternProperties
// for "x-" prefixed fields to any object that has "properties".
// This allows YAML extensions (custom fields starting with x-) to be valid.
func addYAMLExtensions(data map[string]any) {
	if _, hasProperties := data[PropertiesKey]; hasProperties {
		if _, hasPatternProps := data[PatternPropertiesKey]; !hasPatternProps {
			data[PatternPropertiesKey] = map[string]any{
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
