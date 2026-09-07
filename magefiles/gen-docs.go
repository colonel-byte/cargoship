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

package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/cmd"
	"github.com/colonel-byte/cargoship/src/pkg/action"
	"github.com/colonel-byte/cargoship/src/pkg/phase"
	"github.com/colonel-byte/cargoship/src/types/distrocfg"
	"github.com/magefile/mage/mg"
	"github.com/nao1215/markdown"
	"github.com/spf13/cobra/doc"
)

type (
	Generate mg.Namespace
)

// docsConfig is the repo's checked-in config, forced during doc generation so flag defaults
// in generated docs are reproducible regardless of whatever the caller has set locally (e.g.
// via direnv) -- this mirrors the CARGOSHIP_CONFIG=hack/config.yaml override used by the
// pre-commit hook.
const docsConfig = "hack/config.yaml"

// docsConfigChildEnv marks a re-exec'd child process so it runs the real generation logic
// instead of re-exec'ing again.
const docsConfigChildEnv = "CARGOSHIP_MAGE_DOCS_CHILD"

// Document creates the docs for this repo
func (Generate) Document() error {
	// src/cmd sets its flag defaults from CARGOSHIP_CONFIG the moment the package is
	// imported (root.go's package-level `var rootCmd = NewCargoshipCommand()` and its
	// func init() both call initViper(), which is a no-op after the first call). By the
	// time this function body runs, that has already happened -- setting the env var here
	// is too late. Re-exec mage as a child process with the env var set before the child's
	// own cmd package initializes.
	if os.Getenv(docsConfigChildEnv) != "1" {
		c := exec.Command("mage", "generate:document")
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		c.Env = append(os.Environ(), "CARGOSHIP_CONFIG="+docsConfig, docsConfigChildEnv+"=1")
		return c.Run()
	}

	rootCmd := cmd.NewCargoshipCommand()
	rootCmd.DisableAutoGenTag = true

	docsDirs := []string{
		"./docs/commands",
		"./docs/phases",
	}

	for _, dir := range docsDirs {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o775); err != nil {
			return err
		}
	}

	if err := doc.GenMarkdownTreeCustom(rootCmd, "./docs/commands", prependTitle, linkHandler); err != nil {
		return err
	}
	for _, pd := range phaseDocs() {
		if err := writePhaseDoc(pd); err != nil {
			return err
		}
	}
	if err := generateSummary(); err != nil {
		return err
	}

	return nil
}

// gen-docs manager building blocks, reused across the phaseDocs() entries below.
var (
	genDocsManager = &phase.Manager{
		DistroID: distrocfg.DistroRKE2,
		Config: &cluster.ZarfCluster{
			Metadata: cluster.ZarfClusterMetadata{Name: "gen-docs"},
		},
	}
	genDocsManagerNoConfig = &phase.Manager{DistroID: distrocfg.DistroRKE2}
)

// phaseDoc pairs a docs/phases/<name>.md file with the phase list to render into it.
type phaseDoc struct {
	name   string
	phases phase.Phases
	// dryRun adds the dry-run section and per-phase labels to the page. Only apply and reset
	// register the --dry-run flag, so only their pages describe it.
	dryRun bool
}

// phaseDocs lists every action whose phases get a docs/phases/<name>.md page. Add a new
// action's phases here to get it picked up by `Document()` -- no new function needed.
func phaseDocs() []phaseDoc {
	return []phaseDoc{
		{
			name: "apply",
			phases: action.NewApply(action.ApplyOptions{
				Manager: genDocsManager,
			}).Phases,
			dryRun: true,
		},
		{
			name: "reset",
			phases: action.NewReset(action.ResetOptions{
				Manager: genDocsManagerNoConfig,
			}).Phases,
			dryRun: true,
		},
		{
			name: "kube-config",
			phases: action.NewKubeConfig(action.KubeConfigOptions{
				Manager: genDocsManager,
			}).Phases,
		},
		{
			name:   "prepare",
			phases: action.NewPrepare(action.PrepareOptions{}).Phases,
			dryRun: true,
		},
		{
			name: "engine-config-sync",
			phases: action.NewEngineConfigSync(action.EngineConfigSyncOptions{
				Manager: genDocsManager,
			}).Phases,
		},
	}
}

// printExcludedNote marks the sections that docs/css/print.css hides from
// print.html, so the omission is visible to anyone reading SUMMARY.md.
const printExcludedNote = "<!-- Excluded from the print page (print.html) by docs/css/print.css. -->"

func generateSummary() error {
	fmt.Println("docs/SUMMARY.md")
	var builder strings.Builder

	md := markdown.NewMarkdown(&builder)

	// Section order matters beyond the sidebar: docs/css/print.css drops
	// everything from the first Development chapter to the end of the document
	// out of the print page, so Development, Agent and Misc have to stay last,
	// in that order. Anything added after them is excluded from print too.
	summary := []struct {
		title  string
		folder string
		regex  string
		indent bool
		extra  string
		note   string
	}{
		{
			title: "Index",
			extra: "[readme](index.md)",
		},
		{
			title:  "Guides",
			regex:  `(.+)\.md`,
			folder: "guides",
		},
		{
			title:  "Commands",
			extra:  "- [cargoship](commands/cargoship.md)",
			regex:  `cargoship_(.+)\.md`,
			folder: "commands",
			indent: true,
		},
		{
			title:  "Phases",
			regex:  `(.+)\.md`,
			folder: "phases",
		},
		{
			title:  "Development",
			folder: "dev",
			regex:  `(.+)\.md`,
			note:   printExcludedNote,
		},
		{
			title:  "Agent",
			folder: "agent",
			regex:  `(.+)\.md`,
			note:   printExcludedNote,
		},
		{
			title:  "Misc",
			folder: "misc",
			regex:  `(.+)\.md`,
			note:   printExcludedNote,
		},
	}

	for i, item := range summary {
		md = md.H1(item.title)
		md = md.PlainText("")
		if item.note != "" {
			md = md.PlainText(item.note)
			md = md.PlainText("")
		}
		if item.extra != "" {
			md = md.PlainText(item.extra)
		}
		if item.regex != "" && item.folder != "" {
			if mark, err := getMarkdown(item.regex, item.folder, item.indent); err == nil {
				for _, p := range mark {
					md = md.PlainText(p)
				}
			}
		}
		if i != len(summary)-1 {
			md = md.PlainText("\n-----------\n")
		} else {
			md = md.PlainText("")
		}
	}

	if err := md.Build(); err != nil {
		return err
	}

	return os.WriteFile("docs/SUMMARY.md", []byte(builder.String()), 0644)
}

func getMarkdown(pattern string, directory string, indent bool) ([]string, error) {
	list := []string{}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return []string{}, err
	}

	entries, err := os.ReadDir(fmt.Sprintf("docs/%s", directory))
	if err != nil {
		return []string{}, err
	}

	for _, entry := range entries {
		if !entry.IsDir() && re.MatchString(entry.Name()) {
			com := re.FindStringSubmatch(entry.Name())
			if indent {
				list = append(list, fmt.Sprintf("  - [%s](%s/%s)", com[1], directory, entry.Name()))
			} else {
				list = append(list, fmt.Sprintf("- [%s](%s/%s)", com[1], directory, entry.Name()))
			}
		}
	}

	return list, nil
}

func prependTitle(s string) string {
	fmt.Println(s)
	return `<!-- Page generated by Cargoship; DO NOT EDIT -->

`
}

func linkHandler(link string) string {
	return "./" + link[:len(link)-3] + ".md"
}

func phaseComment(mk *markdown.Markdown, p phase.Phase, dryRun bool) {
	// The title and its explanation are one list item, so they go in as one block. Written
	// as two, the writer sees an ordered list followed by a bullet list and separates them
	// with the blank line a new list needs -- which splits every item from its explanation
	// and renders the whole page as a loose list.
	item := fmt.Sprintf("%s\n    - %s", p.Title(), p.Explanation())
	if dryRun {
		// phase.ClassifyDryRun is the same call Manager.Run gates on, so the label is what
		// the phase actually does under --dry-run rather than a second description of it.
		item += fmt.Sprintf("\n    - Dry run: %s", phase.ClassifyDryRun(p))
	}
	mk.OrderedList(item)
}

// dryRunNote heads the pages for the commands that take --dry-run. It covers what the flag does
// to the run as a whole; the per-phase labels below it cover each phase.
const dryRunNote = "With `--dry-run`, cargoship connects to every host and runs the preflight " +
	"checks for real, then reports the phases it would have run instead of running them. Each " +
	"phase below is labelled with what a dry run does with it. A phase is only run when it " +
	"declares that it is safe to, so a phase added later is reported until someone says " +
	"otherwise.\n\n" +
	"The report is not a static list. Every phase still prepares itself and checks whether it " +
	"has anything to do, and both only read, so work that is already done is filtered out " +
	"against the live hosts. A phase that could not be assessed, because it reads state an " +
	"earlier reported phase would have created, is reported as `unassessed`.\n\n" +
	"A dry run takes no cluster lock, so it does not block a real run, and it can report state " +
	"that a concurrent run is already changing. It does not need `--confirm`."

// writePhaseDoc renders one docs/phases/<name>.md page listing each phase's title and explanation.
func writePhaseDoc(pd phaseDoc) error {
	path := fmt.Sprintf("docs/phases/%s.md", pd.name)
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

	doc := markdown.NewMarkdown(f)
	doc.H2(fmt.Sprintf("%s phases", pd.name))

	if pd.dryRun {
		doc.PlainText(dryRunNote)
		doc.PlainTextf("")
	}

	for _, p := range pd.phases {
		phaseComment(doc, p, pd.dryRun)
	}

	doc.PlainTextf("")

	return doc.Build()
}
