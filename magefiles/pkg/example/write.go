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

package example

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/colonel-byte/cargoship/magefiles/pkg/engine/pins"
	"github.com/colonel-byte/cargoship/magefiles/pkg/example/shasums"
)

func write(tmpl *template.Template, repoURL, tag string, spec distroSpec, f flavor, sums *shasums.Cache) (string, error) {
	v, err := newVersion(tag, spec, f)
	if err != nil {
		return "", err
	}

	minor, err := pins.TagMinor(tag)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(f.dir, minor, "v"+v.Version)

	if spec.probe != nil {
		published, err := sums.Published(spec.probe(v))
		if err != nil {
			return "", err
		}
		if !published {
			return "", removeDir(dir)
		}
	}

	// Only now, once the build is known to be installable, spend the release fetches.
	if err := v.fetchImages(repoURL, spec, f); err != nil {
		return "", err
	}
	if spec.fetch != nil {
		if err := spec.fetch(&v, repoURL); err != nil {
			return "", err
		}
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, v); err != nil {
		return "", err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	path := filepath.Join(dir, "distro.yaml")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return "", err
	}

	if err := writeValues(dir, f, v); err != nil {
		return "", err
	}
	return path, nil
}

// writeValues renders a flavor's values templates next to its distro.yaml, under the
// name the distro.yaml refers to them by -- the template's own name without the .tmpl.
//
// The values a package ships are files beside its definition rather than part of it, so a
// flavor that exposes any has more than one file to render. They are rendered from the same
// data as the definition, so a value that has to agree with the definition can be written
// once and used in both.
func writeValues(dir string, f flavor, v version) error {
	// The schema suggests addons.disabled values from the extracted vocabulary, so a flavor
	// whose version has none would ship a values file an editor can offer nothing for. That
	// is a version whose source was never pulled rather than a package with no addons, so it
	// is a failure to report here. Pull that version's source (mage
	// generate:pullEngineSource) and regenerate.
	if len(f.values) > 0 && len(v.Addons) == 0 {
		return fmt.Errorf("no generated addon vocabulary for %s %s: run mage generate:pullEngineSource and mage generate:engineConfig for that minor line", v.Name, v.Version)
	}

	for _, path := range f.values {
		tmpl, err := template.New(filepath.Base(path)).ParseFiles(path)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, v); err != nil {
			return fmt.Errorf("rendering %s: %w", path, err)
		}
		name := strings.TrimSuffix(filepath.Base(path), ".tmpl")
		if err := os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0o644); err != nil {
			return err
		}
	}
	return nil
}
