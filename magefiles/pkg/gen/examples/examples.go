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

// This file holds how an example is written: the template it is rendered with, the distro.yaml
// and values files it becomes on disk, and the removal of an example upstream no longer
// publishes. What it is rendered from is split across distros.go, flavor.go, and version.go;
// which tags get rendered is in tags.go.

// Package examples renders the example distro.yaml packages under example/ from the shared
// templates, one per distro flavor and release tag.
package examples

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/engineconfig"
)

// parseExampleTemplate parses a distro's example template, wiring in the sha256 function its
// remote file entries are hashed with.
func parseExampleTemplate(spec exampleDistroSpec, sums *exampleShasums) (*template.Template, error) {
	tmpl, err := template.New(filepath.Base(spec.template)).
		Funcs(template.FuncMap{"sha256": sums.get}).
		ParseFiles(spec.template)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", spec.template, err)
	}
	return tmpl, nil
}

// writeExample renders one tag into <flavor dir>/<minor>/<tag>/distro.yaml, creating the
// minor line directory if this is its first example, and returns the path it wrote.
//
// It returns "" for a build upstream no longer publishes, having removed any example
// already on disk for it. Rancher supersedes an rke2rN with the next revision and drops the
// old RPMs -- v1.35.0+rke2r2 and v1.35.3+rke2r2 both gave way to an rke2r3 -- and an example
// pointing at RPM URLs that 404 cannot be built, so keeping it only invites someone to try.
// The git tag surviving is what makes these renderable in the first place, so it is the
// distro's own artifacts that get checked.
func writeExample(tmpl *template.Template, repoURL, tag string, spec exampleDistroSpec, f exampleFlavor, sums *exampleShasums) (string, error) {
	v, err := newExampleVersion(tag, spec, f)
	if err != nil {
		return "", err
	}

	minor, err := engineconfig.TagMinor(tag)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(f.dir, minor, "v"+v.Version)

	if spec.probe != nil {
		published, err := sums.published(spec.probe(v))
		if err != nil {
			return "", err
		}
		if !published {
			return "", removeExample(dir)
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

	if err := writeExampleValues(dir, f, v); err != nil {
		return "", err
	}
	return path, nil
}

// writeExampleValues renders a flavor's values templates next to its distro.yaml, under the
// name the distro.yaml refers to them by -- the template's own name without the .tmpl.
//
// The values a package ships are files beside its definition rather than part of it, so a
// flavor that exposes any has more than one file to render. They are rendered from the same
// data as the definition, so a value that has to agree with the definition can be written
// once and used in both.
func writeExampleValues(dir string, f exampleFlavor, v exampleVersion) error {
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

// removeExample deletes an example directory, and the minor line directory with it if that
// leaves it empty. Missing is fine -- the point is that it is gone.
func removeExample(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	// Fails while the line still holds examples, which is exactly when it should stay.
	os.Remove(filepath.Dir(dir))
	return nil
}
