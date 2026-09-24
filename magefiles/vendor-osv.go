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

//go:build mage
// +build mage

package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// osvOverride is one osv-scanner.toml that has to exist inside vendor/ for the OpenSSF
// Scorecard Vulnerabilities check to describe this repository rather than its dependencies'
// dependencies.
//
// Some Go modules keep a lockfile for a language cargoship does not build in: x/telemetry has
// a package-lock.json for the web UI it serves out of its own repository, go-tuf and txeh have
// requirements files for a test harness and a documentation site. go mod vendor copies those
// in alongside the Go source because they sit in the module's directory, and Scorecard then
// runs osv-scanner recursively over the whole checkout and scores Vulnerabilities as ten minus
// the number of findings. Forty-odd advisories against JavaScript and Python that cargoship
// never compiles, executes or ships is what held that check at zero.
//
// The override is ecosystem-wide rather than a list of advisory IDs, because the point is that
// the ecosystem is absent from the build, not that today's advisories happen to be harmless.
// An ID list would need editing every time a new CVE landed in a lockfile nobody here reads.
//
// It has to sit beside the manifest. osv-scanner resolves its config relative to the file it
// is scanning, and Scorecard passes no ConfigPath, so an osv-scanner.toml at the repository
// root is read by nothing -- verified against scorecard v5.5.0, where the root file changed a
// 42-finding scan by zero and these four changed it to 3.
type osvOverride struct {
	// dir is the directory holding the manifest, relative to the repository root. The
	// generated osv-scanner.toml goes here.
	dir string
	// manifest is the file whose findings this override suppresses. Named only so the
	// generated comment can say what the file is for.
	manifest string
	// ecosystem is the OSV ecosystem identifier, spelled as osv-scanner spells it.
	ecosystem string
	// what describes the manifest in a sentence continuing "<manifest> here is ...".
	what string
}

// osvOverrides is the full set. Every entry corresponds to a non-Go manifest that go mod vendor
// currently copies in; Dev.VerifyVendor fails if vendor/ grows one this does not cover.
var osvOverrides = []osvOverride{
	{
		dir:       "vendor/golang.org/x/telemetry",
		manifest:  "package-lock.json",
		ecosystem: "npm",
		what:      "the devDependencies of the web UI x/telemetry serves from its own repository",
	},
	{
		dir:       "vendor/github.com/theupdateframework/go-tuf",
		manifest:  "requirements-test.txt",
		ecosystem: "PyPI",
		what:      "the Python harness go-tuf runs its own conformance tests under",
	},
	{
		dir:       "vendor/github.com/txn2/txeh",
		manifest:  "requirements-docs.txt",
		ecosystem: "PyPI",
		what:      "the MkDocs toolchain that builds txeh's documentation site",
	},
	{
		dir:       "vendor/go.opentelemetry.io/otel",
		manifest:  "requirements.txt",
		ecosystem: "PyPI",
		what:      "the codespell pin otel's own spelling check installs",
	},
}

// osvOverrideName is the filename osv-scanner looks for next to a manifest.
const osvOverrideName = "osv-scanner.toml"

// osvEcosystemLanguage names the language an OSV ecosystem packages, for the generated reason.
func osvEcosystemLanguage(ecosystem string) string {
	switch ecosystem {
	case "npm":
		return "JavaScript"
	case "PyPI":
		return "Python"
	default:
		return ecosystem
	}
}

// osvCommentWidth is the column the generated comment wraps at, counting the "# ".
const osvCommentWidth = 96

// osvComment renders text as a wrapped TOML comment block. The paragraphs are built by
// substitution, so they cannot be wrapped in the source the way a fixed string would be.
func osvComment(paragraphs ...string) string {
	var b strings.Builder
	for i, p := range paragraphs {
		if i > 0 {
			b.WriteString("#\n")
		}
		line := "#"
		for _, word := range strings.Fields(p) {
			if len(line)+1+len(word) > osvCommentWidth {
				b.WriteString(line + "\n")
				line = "#"
			}
			line += " " + word
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// osvOverrideContent renders the osv-scanner.toml for one override. The comment is addressed to
// whoever finds the file while reading vendored third-party source and wonders who put it there.
func osvOverrideContent(o osvOverride) string {
	return osvComment(
		`Generated by "mage dev:vendor". Do not edit, and do not delete.`,
		fmt.Sprintf(`%s here is %s. It is not a cargoship dependency -- go mod vendor copies it in `+
			`because it sits in a vendored module's directory, and nothing it names is compiled, `+
			`executed or shipped.`, o.manifest, o.what),
		`OpenSSF Scorecard runs osv-scanner over the whole checkout and scores Vulnerabilities as `+
			`ten minus the number of findings, so without this file every advisory ever filed `+
			`against those packages costs the repository a point. See `+
			`docs/agent/choice-osv-vendor-overrides.md.`,
	) + fmt.Sprintf(`
[[PackageOverrides]]
ecosystem = %q
ignore = true
reason = %q
`,
		o.ecosystem,
		osvEcosystemLanguage(o.ecosystem)+" tooling vendored alongside a Go module; not built, run or shipped by cargoship",
	)
}

// WriteOSVOverrides writes every override into vendor/. go mod vendor deletes and recreates the
// tree, so this runs after it every time rather than relying on the files surviving.
func (Dev) WriteOSVOverrides() error {
	for _, o := range osvOverrides {
		if _, err := os.Stat(filepath.Join(o.dir, o.manifest)); err != nil {
			return fmt.Errorf("osv override for %s: %w -- the manifest is gone, so drop the entry from magefiles/vendor-osv.go", o.dir, err)
		}
		path := filepath.Join(o.dir, osvOverrideName)
		if err := os.WriteFile(path, []byte(osvOverrideContent(o)), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
		fmt.Println("wrote", path)
	}
	return nil
}

// osvForeignManifest reports whether name is a dependency manifest for an ecosystem other than
// Go: the subset of osv-scanner's extractors that can plausibly turn up inside a Go module's
// source tree. Anything matching has to be covered by an entry in osvOverrides, or it scores
// against us the moment somebody files an advisory on one of the packages it pins.
func osvForeignManifest(name string) bool {
	switch name {
	case "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "bun.lock",
		"Pipfile.lock", "poetry.lock", "pdm.lock", "uv.lock",
		"Gemfile.lock", "Cargo.lock", "composer.lock", "mix.lock",
		"pubspec.lock", "conan.lock", "gradle.lockfile", "packages.lock.json",
		"pom.xml":
		return true
	}
	return strings.Contains(name, "requirements") && strings.HasSuffix(name, ".txt")
}

// VerifyVendor checks that vendor/ still says what Dev.Vendor wrote: every declared
// osv-scanner.toml present and byte-identical, and no foreign manifest that nothing covers.
//
// The second half is the one that earns its keep. A dependency bump can pull in a module that
// happens to carry a lockfile, and the Vulnerabilities score then falls on some later Scorecard
// run with nothing in the diff to explain it. This turns that into a failure on the commit that
// caused it.
func (Dev) VerifyVendor() error {
	var problems []string

	covered := make(map[string]string, len(osvOverrides))
	for _, o := range osvOverrides {
		covered[filepath.Clean(o.dir)] = o.ecosystem

		path := filepath.Join(o.dir, osvOverrideName)
		got, err := os.ReadFile(path)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s is missing (%v)", path, err))
			continue
		}
		if string(got) != osvOverrideContent(o) {
			problems = append(problems, fmt.Sprintf("%s does not match magefiles/vendor-osv.go", path))
		}
	}

	err := filepath.WalkDir("vendor", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !osvForeignManifest(d.Name()) {
			return nil
		}
		if _, ok := covered[filepath.Dir(path)]; !ok {
			problems = append(problems, fmt.Sprintf("%s is a non-Go dependency manifest with no entry in osvOverrides", path))
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walking vendor: %w", err)
	}

	if len(problems) > 0 {
		// The two kinds of problem want different things. A missing or drifted override is
		// regenerated; an uncovered manifest cannot be, because nothing here knows which
		// ecosystem it belongs to or how to describe it. Saying `mage dev:vendor` for both sent
		// whoever hit the second one off to re-vendor a tree that was already correct.
		return fmt.Errorf("vendored osv-scanner overrides are out of date:\n  %s\n"+
			"a missing or mismatched override is rewritten by `mage dev:writeOSVOverrides`; "+
			"an uncovered manifest needs an entry in osvOverrides in magefiles/vendor-osv.go first",
			strings.Join(problems, "\n  "))
	}
	fmt.Printf("%d osv-scanner overrides present and current\n", len(osvOverrides))
	return nil
}
