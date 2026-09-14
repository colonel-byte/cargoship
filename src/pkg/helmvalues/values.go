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
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/colonel-byte/cargoship/src/pkg/utils"
	"github.com/go-sprout/sprout/group/all"
	"github.com/go-sprout/sprout/sprigin"
	"github.com/k0sproject/dig"
	"gopkg.in/yaml.v3"
)

// FuncMap returns the sprout template FuncMap extended with cargoship custom helpers.
func FuncMap() template.FuncMap {
	_ = all.RegistryGroup() // reference group if needed
	fm := sprigin.FuncMap()
	if fm == nil {
		fm = make(template.FuncMap)
	}

	// fileExists checks if a path exists on the local filesystem.
	fm["fileExists"] = func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	}

	// cachedFileExists checks if a file exists under cargoship's cache directory (or custom relative cache path).
	fm["cachedFileExists"] = func(relPath string) bool {
		cacheRoot, err := utils.ResolveCachePath("")
		if err != nil {
			return false
		}
		target := filepath.Join(cacheRoot, relPath)
		_, err = os.Stat(target)
		return err == nil
	}

	// cachedFilePath returns the absolute path to a file in the cargoship cache directory.
	fm["cachedFilePath"] = func(relPath string) string {
		cacheRoot, err := utils.ResolveCachePath("")
		if err != nil {
			return relPath
		}
		return filepath.Join(cacheRoot, relPath)
	}

	// downloadToCache downloads a URL into cargoship's cache directory and returns the absolute local path.
	// Optionally takes an expected SHA256 checksum as a 3rd parameter.
	fm["downloadToCache"] = func(url, relPath string, args ...string) (string, error) {
		return utils.DownloadToCache(url, relPath, args...)
	}

	return fm
}

// RenderTemplate evaluates a Go template string using sprout template functions.
func RenderTemplate(tmplStr string, data any) (string, error) {
	if !strings.Contains(tmplStr, "{{") {
		return tmplStr, nil
	}
	tmpl, err := template.New("values").Funcs(FuncMap()).Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}
	return buf.String(), nil
}

// EvaluateValuesTemplates recursively renders template strings inside any map or slice structure.
func EvaluateValuesTemplates(v any, data any) (any, error) {
	switch val := v.(type) {
	case string:
		rendered, err := RenderTemplate(val, data)
		if err != nil {
			return nil, err
		}
		// If the evaluated string was a template output representing a boolean, number, or array, parse to scalar/typed structure.
		if strings.Contains(val, "{{") {
			return parseScalarOrList(strings.TrimSpace(rendered)), nil
		}
		return rendered, nil
	case dig.Mapping:
		res := make(dig.Mapping, len(val))
		for k, item := range val {
			evaledKey, err := RenderTemplate(k, data)
			if err != nil {
				return nil, err
			}
			evaledVal, err := EvaluateValuesTemplates(item, data)
			if err != nil {
				return nil, err
			}
			res[evaledKey] = evaledVal
		}
		return res, nil
	case map[string]any:
		res := make(map[string]any, len(val))
		for k, item := range val {
			evaledKey, err := RenderTemplate(k, data)
			if err != nil {
				return nil, err
			}
			evaledVal, err := EvaluateValuesTemplates(item, data)
			if err != nil {
				return nil, err
			}
			res[evaledKey] = evaledVal
		}
		return res, nil
	case map[any]any:
		res := make(map[string]any, len(val))
		for k, item := range val {
			kStr := fmt.Sprint(k)
			evaledKey, err := RenderTemplate(kStr, data)
			if err != nil {
				return nil, err
			}
			evaledVal, err := EvaluateValuesTemplates(item, data)
			if err != nil {
				return nil, err
			}
			res[evaledKey] = evaledVal
		}
		return res, nil
	case []any:
		res := make([]any, len(val))
		for i, item := range val {
			evaled, err := EvaluateValuesTemplates(item, data)
			if err != nil {
				return nil, err
			}
			res[i] = evaled
		}
		return res, nil
	default:
		return v, nil
	}
}

// ParseSet parses a Helm-style --set key=value string into a nested map[string]any structure.
func ParseSet(s string) (map[string]any, error) {
	result := make(map[string]any)
	parts := splitSetEntries(s)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		eqIdx := strings.Index(part, "=")
		if eqIdx == -1 {
			return nil, fmt.Errorf("invalid set format %q (missing '=')", part)
		}
		key := strings.TrimSpace(part[:eqIdx])
		valStr := strings.TrimSpace(part[eqIdx+1:])
		val := parseScalarOrList(valStr)

		if err := setPath(result, key, val); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func splitSetEntries(s string) []string {
	var entries []string
	var current strings.Builder
	inBracket := false
	inQuote := false
	var quoteChar rune

	for _, r := range s {
		switch r {
		case '[', '{':
			if !inQuote {
				inBracket = true
			}
			current.WriteRune(r)
		case ']', '}':
			if !inQuote {
				inBracket = false
			}
			current.WriteRune(r)
		case '"', '\'':
			if inQuote && r == quoteChar {
				inQuote = false
			} else if !inQuote {
				inQuote = true
				quoteChar = r
			}
			current.WriteRune(r)
		case ',':
			if !inBracket && !inQuote {
				entries = append(entries, current.String())
				current.Reset()
			} else {
				current.WriteRune(r)
			}
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		entries = append(entries, current.String())
	}
	return entries
}

func parseScalarOrList(val string) any {
	if val == "true" {
		return true
	}
	if val == "false" {
		return false
	}
	if i, err := strconv.ParseInt(val, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(val, 64); err == nil && strings.Contains(val, ".") {
		return f
	}
	if strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]") {
		inner := strings.Trim(val[1:len(val)-1], " ")
		if inner == "" {
			return []any{}
		}
		rawItems := strings.Split(inner, ",")
		items := make([]any, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, parseScalarOrList(strings.TrimSpace(item)))
		}
		return items
	}
	return val
}

func setPath(dst map[string]any, path string, val any) error {
	keys := strings.Split(path, ".")
	curr := dst
	for i := 0; i < len(keys)-1; i++ {
		k := keys[i]
		if next, ok := curr[k]; ok {
			if nextMap, ok := next.(map[string]any); ok {
				curr = nextMap
			} else {
				return fmt.Errorf("path conflict at key %q", k)
			}
		} else {
			nextMap := make(map[string]any)
			curr[k] = nextMap
			curr = nextMap
		}
	}
	curr[keys[len(keys)-1]] = val
	return nil
}

// MergeValues merges src map into dst map recursively.
func MergeValues(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = make(map[string]any)
	}
	for k, v := range src {
		if srcMap, ok := v.(map[string]any); ok {
			if dstMap, ok := dst[k].(map[string]any); ok {
				dst[k] = MergeValues(dstMap, srcMap)
				continue
			}
		}
		dst[k] = v
	}
	return dst
}

// RenderValues renders any value (string, map, slice, scalar) into YAML values content.
func RenderValues(v any) (string, error) {
	if s, ok := v.(string); ok {
		return s, nil
	}

	buf := bytes.Buffer{}
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}
