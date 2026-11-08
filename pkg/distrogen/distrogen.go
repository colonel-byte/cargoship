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

// Package distrogen renders package definitions - a distro.yaml and whatever files sit beside
// it - from Go templates.
//
// It is the generator both callers share: `cargoship dev render`, where an operator brings
// their own template and a values file, and mage's example targets, which derive their data
// from engine pins and render the committed examples under example/. The templates are evaluated
// with the same function map a package's own values templates get (pkg/helmvalues), so a
// template written for one renders the same way in the other.
//
// Rendering and writing are separate steps because a caller may need the bytes first: the
// example generator drops a rendered content manifest that turns out to declare nothing.
package distrogen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/colonel-byte/cargoship/pkg/helmvalues"
	"github.com/colonel-byte/cargoship/pkg/schema"
)

// File is one template and the name it renders to.
type File struct {
	Template string // path to the template
	Name     string // output name; "" means the template's base name without .tmpl
}

// Rendered is one template's output, before anything has been written.
type Rendered struct {
	Name    string
	Content []byte
}

// Render evaluates each file's template against data. The returned slice is in the order the
// files were given.
func Render(files []File, data any, opts ...helmvalues.Option) ([]Rendered, error) {
	out := make([]Rendered, 0, len(files))
	for _, f := range files {
		b, err := os.ReadFile(f.Template)
		if err != nil {
			return nil, err
		}
		content, err := helmvalues.RenderTemplate(string(b), data, opts...)
		if err != nil {
			return nil, fmt.Errorf("rendering %s: %w", f.Template, err)
		}
		out = append(out, Rendered{Name: f.OutputName(), Content: []byte(content)})
	}
	return out, nil
}

// OutputName is the name a file renders to: what it declares, or its template's base name
// without the .tmpl suffix.
func (f File) OutputName() string {
	if f.Name != "" {
		return f.Name
	}
	return strings.TrimSuffix(filepath.Base(f.Template), ".tmpl")
}

// CheckOutputs reports whether rendering files into dir would write somewhere it must not.
//
// Two of these have eaten a file in practice. An output that lands on one of the templates
// being rendered overwrites it with its own output - `dev render -t ./distro.yaml` with no
// --out is the whole of it, since reading happens before writing and nothing fails. Two
// templates whose names collide write to one path, and the first result is lost behind the
// second while both are reported as rendered.
//
// Called before Render rather than before Write so that nothing is read, let alone written,
// when the request cannot be satisfied.
func CheckOutputs(dir string, files []File) error {
	seen := make(map[string]string, len(files))
	templates := make(map[string]string, len(files))

	for _, f := range files {
		abs, err := filepath.Abs(f.Template)
		if err != nil {
			return err
		}
		templates[abs] = f.Template
	}

	for _, f := range files {
		name := f.OutputName()
		if name == "" || name == "." || strings.ContainsRune(name, filepath.Separator) {
			return fmt.Errorf("%s renders to %q, which is not a file name", f.Template, name)
		}

		abs, err := filepath.Abs(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		if other, ok := templates[abs]; ok {
			return fmt.Errorf("rendering %s would write over %s, the template itself: name an --out directory that is not the one the templates are in", f.Template, other)
		}
		if other, ok := seen[abs]; ok {
			return fmt.Errorf("%s and %s both render to %s", other, f.Template, name)
		}
		seen[abs] = f.Template
	}

	return nil
}

// Write writes rendered output into dir, creating it if it does not exist, and returns the
// paths it wrote.
func Write(dir string, rendered []Rendered) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(rendered))
	for _, r := range rendered {
		path := filepath.Join(dir, r.Name)
		if err := os.WriteFile(path, r.Content, 0o644); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

// ValidateDistro checks rendered bytes against the generated package schema, the same check
// `cargoship validate` applies to a definition already on disk. source names the document in
// the problems it reports.
func ValidateDistro(source string, content []byte) error {
	doc, err := schema.Decode(content)
	if err != nil {
		return fmt.Errorf("%s: %w", source, err)
	}
	v, err := schema.NewValidatorFor(schema.KindPackage)
	if err != nil {
		return err
	}
	return v.Validate(source, doc)
}
