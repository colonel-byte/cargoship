// Copyright 2021 zarf authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from zarf:
// https://github.com/zarf-dev/zarf
//
// Modifications Copyright 2026 colonel-byte.
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

// Package cfg is used to parse an byte array and returns a ZarfDistro
package cfg

import (
	"context"
	"errors"
	"fmt"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	goyaml "github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// apiVersionHandler pairs a supported apiVersion with its decoder.
type apiVersionHandler struct {
	version  string
	priority int
	decode   func(ctx context.Context, node ast.Node) (distro.ZarfDistro, error)
}

// knownAPIVersions lists every apiVersion this Cargoship version can decode. To add a
// new version, append an entry with a higher priority than any existing one.
var knownAPIVersions = []apiVersionHandler{
	{version: v1alpha1.APIVersion, priority: 1, decode: decodeV1Alpha1},
}

// Parse parses the yaml passed as a byte slice and applies schema migrations.
func Parse(ctx context.Context, b []byte) (distro.ZarfDistro, error) {
	docs, err := parseDistroYAMLDocs(b)
	if err != nil {
		return distro.ZarfDistro{}, err
	}
	if len(docs) > 1 {
		return distro.ZarfDistro{}, errors.New("package definition must contain a single YAML document")
	}
	version, err := apiVersionFromNode(docs[0].Body)
	if err != nil {
		return distro.ZarfDistro{}, fmt.Errorf("reading apiVersion: %w", err)
	}
	handler, known := handlerFor(version)
	if !known {
		return distro.ZarfDistro{}, fmt.Errorf("unsupported apiVersion %q", version)
	}
	return handler.decode(ctx, docs[0].Body)
}

// ParseMultiDoc parses a multi doc zarf.yaml file, generally from an already built package.
// Multi doc definitions may contain one document per apiVersion.
func ParseMultiDoc(ctx context.Context, b []byte) (distro.ZarfDistro, error) {
	l := logger.From(ctx)
	docs, err := parseDistroYAMLDocs(b)
	if err != nil {
		return distro.ZarfDistro{}, err
	}

	var (
		chosen     apiVersionHandler
		chosenNode ast.Node
		found      bool
	)
	seenVersions := map[string]bool{}

	for i, doc := range docs {
		version, err := apiVersionFromNode(doc.Body)
		if err != nil {
			return distro.ZarfDistro{}, fmt.Errorf("document %d: reading apiVersion: %w", i, err)
		}
		handler, known := handlerFor(version)
		if !known {
			l.Debug("found unsupported API version during parse", "apiVersion", version)
			continue
		}
		if seenVersions[handler.version] {
			return distro.ZarfDistro{}, fmt.Errorf("duplicate apiVersion %q in package definition", handler.version)
		}
		seenVersions[handler.version] = true
		if !found || handler.priority > chosen.priority {
			chosen = handler
			chosenNode = doc.Body
			found = true
		}
	}

	if !found {
		return distro.ZarfDistro{}, errors.New("no supported apiVersion found in package definition")
	}
	return chosen.decode(ctx, chosenNode)
}

func decodeV1Alpha1(_ context.Context, node ast.Node) (distro.ZarfDistro, error) {
	var pkg distro.ZarfDistro
	if err := goyaml.NodeToValue(node, &pkg); err != nil {
		return distro.ZarfDistro{}, err
	}
	return pkg, nil
}

func handlerFor(version string) (apiVersionHandler, bool) {
	if version == "" {
		version = v1alpha1.APIVersion
	}
	for _, h := range knownAPIVersions {
		if h.version == version {
			return h, true
		}
	}
	return apiVersionHandler{}, false
}

func apiVersionFromNode(node ast.Node) (string, error) {
	if node == nil {
		return "", nil
	}
	var probe struct {
		APIVersion string `yaml:"apiVersion"`
	}
	if err := goyaml.NodeToValue(node, &probe); err != nil {
		return "", err
	}
	return probe.APIVersion, nil
}

func parseDistroYAMLDocs(b []byte) ([]*ast.DocumentNode, error) {
	file, err := parser.ParseBytes(b, 0)
	if err != nil {
		return nil, err
	}
	docs := filterEmptyDocs(file.Docs)
	if len(docs) == 0 {
		return nil, errors.New("no package definition found")
	}
	return docs, nil
}

func filterEmptyDocs(docs []*ast.DocumentNode) []*ast.DocumentNode {
	out := make([]*ast.DocumentNode, 0, len(docs))
	for _, d := range docs {
		if d == nil || d.Body == nil {
			continue
		}
		out = append(out, d)
	}
	return out
}
