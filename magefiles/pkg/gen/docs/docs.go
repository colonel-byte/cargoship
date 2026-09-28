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

// Package docs runs every generator the book is built from, in the order their output depends on:
// the pages first, then gen/summary, which indexes whatever the others wrote.
package docs

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/colonel-byte/cargoship/cmd"
	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/ansibledoc"
	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/book"
	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/golangdoc"
	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/page"
	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/phasedoc"
	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/schemadoc"
	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/summary"
	"github.com/spf13/cobra/doc"
)

// commandsDir holds the pages cobra generates, one per command.
const commandsDir = "docs/commands"

// configPath is the repo's checked-in config, forced during doc generation so flag defaults
// in generated docs are reproducible regardless of whatever the caller has set locally (e.g.
// via direnv) -- this mirrors the CARGOSHIP_CONFIG=hack/config.yaml override used by the
// pre-commit hook.
const configPath = "hack/config.yaml"

// childEnv marks a re-exec'd child process so it runs the real generation logic
// instead of re-exec'ing again.
const childEnv = "CARGOSHIP_MAGE_DOCS_CHILD"

// Generate writes every generated page under docs/.
func Generate() error {
	// cmd sets its flag defaults from CARGOSHIP_CONFIG the moment the package is
	// imported (root.go's package-level `var rootCmd = NewCargoshipCommand()` and its
	// func init() both call initViper(), which is a no-op after the first call). By the
	// time this function body runs, that has already happened -- setting the env var here
	// is too late. Re-exec mage as a child process with the env var set before the child's
	// own cmd package initializes.
	if os.Getenv(childEnv) != "1" {
		c := exec.Command("go", "run", "magefiles/core", "generate:document")
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		c.Env = append(os.Environ(), "CARGOSHIP_CONFIG="+configPath, childEnv+"=1")
		return c.Run()
	}

	rootCmd := cmd.NewCargoshipCommand()
	rootCmd.DisableAutoGenTag = true

	docsDirs := []string{
		"./" + commandsDir,
		"./" + phasedoc.Dir,
		"./" + golangdoc.Dir,
		"./" + schemadoc.Dir,
	}

	for _, dir := range docsDirs {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o775); err != nil {
			return err
		}
	}

	if err := doc.GenMarkdownTreeCustom(rootCmd, "./"+commandsDir, prependTitle, linkHandler); err != nil {
		return err
	}
	if err := phasedoc.Generate(); err != nil {
		return err
	}
	if err := schemadoc.Generate(); err != nil {
		return err
	}
	if err := ansibledoc.Generate(); err != nil {
		return err
	}
	if err := golangdoc.Generate(); err != nil {
		return err
	}
	if err := book.Generate(); err != nil {
		return err
	}
	return summary.Generate()
}

// prependTitle is cobra's per-file header hook. It is also the only place a command page's name
// is known, which is why the progress line is printed from here.
func prependTitle(s string) string {
	fmt.Println(s)
	return page.Banner + "\n\n"
}

// linkHandler rewrites cobra's cross-references between command pages, which it emits without the
// leading "./" mdBook needs.
func linkHandler(link string) string {
	return "./" + link[:len(link)-3] + ".md"
}
