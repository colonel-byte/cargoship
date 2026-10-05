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

// Package schema serves the JSON Schema documents cargoship generates for its own
// file formats, out of the binary rather than over the network. The documents under
// embedded/ are byte-identical copies of the repository's schema/ directory, written
// by the same `mage generate:schema` target; go:embed cannot reach outside its own
// package directory, which is why the copy exists at all.
package schema

import (
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"sort"
)

//go:embed embedded/*.json
var embedded embed.FS

// Kind names one of the file formats cargoship generates a schema for.
type Kind string

const (
	// KindInventory is the cluster inventory a `--config` flag points at.
	KindInventory Kind = "inventory"
	// KindPackage is a package definition, a distro.yaml.
	KindPackage Kind = "package"
	// KindConfig is a cargoship CLI configuration file.
	KindConfig Kind = "config"
)

// fileNames maps a kind onto the generated document that describes it. The names are
// the ones in the repository's schema/ directory, so a document served from the binary
// and the same document fetched from GitHub are interchangeable.
var fileNames = map[Kind]string{
	KindInventory: "zarf-v1alpha1-cluster-schema.json",
	KindPackage:   "zarf-v1alpha1-distro-package-schema.json",
	KindConfig:    "zarf-config-distro-schema.json",
}

// Kinds returns every kind that can be served, sorted, for flag validation and shell
// completion.
func Kinds() []string {
	out := make([]string, 0, len(fileNames))
	for k := range fileNames {
		out = append(out, string(k))
	}
	sort.Strings(out)
	return out
}

// FileName returns the name the document carries in the repository, so a caller writing
// it to disk can use the name an editor's $schema reference already expects.
func FileName(kind Kind) (string, error) {
	name, ok := fileNames[kind]
	if !ok {
		return "", fmt.Errorf("unknown schema %q, expected one of %v", kind, Kinds())
	}
	return name, nil
}

// Raw returns the generated document for a kind, exactly as it sits in the repository,
// trailing newline included.
func Raw(kind Kind) ([]byte, error) {
	name, err := FileName(kind)
	if err != nil {
		return nil, err
	}
	b, err := embedded.ReadFile(path.Join("embedded", name))
	if err != nil {
		return nil, fmt.Errorf("unable to read embedded schema %s: %w", name, err)
	}
	return b, nil
}

// Load returns the generated document for a kind, decoded, for callers that need to
// compose it rather than hand it straight to an editor.
func Load(kind Kind) (map[string]any, error) {
	b, err := Raw(kind)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("unable to parse embedded schema %s: %w", kind, err)
	}
	return doc, nil
}
