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

package docs

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/pkg/action"
	"github.com/colonel-byte/cargoship/pkg/phase"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/nao1215/markdown"
)

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
	// dryRun adds the dry-run section and per-phase labels to the page. Set it for every
	// action that registers the --dry-run flag, so a page describes the flag exactly when
	// the command accepts it.
	dryRun bool
}

// phaseDocs lists every action whose phases get a docs/phases/<name>.md page. Add a new
// action's phases here to get it picked up by `Document()` -- no new function needed.
//
// The action constructors return an error for a distro ID they cannot resolve, and the managers
// below name a real one, so an error here is the generator being wrong rather than an input being
// wrong: it comes back and `Document()` fails with it, instead of rendering a page from a nil
// action's empty phase list.
func phaseDocs() ([]phaseDoc, error) {
	apply, err := action.NewApply(action.ApplyOptions{
		Manager: genDocsManager,
	})
	if err != nil {
		return nil, err
	}
	reset, err := action.NewReset(action.ResetOptions{
		Manager: genDocsManagerNoConfig,
	})
	if err != nil {
		return nil, err
	}
	kubeConfig, err := action.NewKubeConfig(action.KubeConfigOptions{
		Manager: genDocsManager,
	})
	if err != nil {
		return nil, err
	}
	refresh, err := action.NewRefresh(action.RefreshOptions{
		Manager: genDocsManager,
	})
	if err != nil {
		return nil, err
	}
	sync, err := action.NewEngineConfigSync(action.EngineConfigSyncOptions{
		Manager: genDocsManager,
	})
	if err != nil {
		return nil, err
	}

	return []phaseDoc{
		{
			name:   "apply",
			phases: apply.Phases,
			dryRun: true,
		},
		{
			name:   "reset",
			phases: reset.Phases,
			dryRun: true,
		},
		{
			name:   "kube-config",
			phases: kubeConfig.Phases,
		},
		{
			name:   "prepare",
			phases: action.NewPrepare(action.PrepareOptions{}).Phases,
			dryRun: true,
		},
		{
			name:   "refresh",
			phases: refresh.Phases,
		},
		{
			name:   "engine-config-sync",
			phases: sync.Phases,
			dryRun: true,
		},
	}, nil
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
		// lines supplies the section's entries itself, for a section the folder walk below
		// cannot produce. It replaces regex and folder rather than adding to them.
		lines func() ([]string, error)
	}{
		{
			// Both entries are prefix chapters: mdBook gives a line before the first list
			// item no number and places it ahead of every numbered chapter, which is where
			// the front page and the security policy belong.
			title: "Index",
			extra: "[readme](index.md)\n[security](security.md)",
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
			title:  "Schema",
			regex:  `(.+)\.md`,
			folder: "schema",
		},
		{
			title: "Ansible",
			lines: ansibleSummary,
		},
		{
			title:  "Phases",
			regex:  `(.+)\.md`,
			folder: "phases",
		},
		{
			title: "Golang",
			lines: golangSummary,
		},
		{
			title:  "Development",
			folder: "dev",
			regex:  `(.+)\.md`,
			note:   printExcludedNote,
		},
		{
			title:  "Workflows",
			folder: "workflows",
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
		if item.lines != nil {
			// Unlike the folder walk below, this error is returned rather than swallowed: a
			// section that supplies its own entries has nothing to fall back to, and an empty
			// chapter is not something anyone would notice in the diff.
			mark, err := item.lines()
			if err != nil {
				return err
			}
			for _, p := range mark {
				md = md.PlainText(p)
			}
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
				// Cobra names a subcommand's page after its whole command path, joined with
				// underscores, so "vault encrypt" lands in cargoship_vault_encrypt.md. Step
				// the bullet in one level per ancestor and label it with the last word, so
				// that the sidebar nests a command under the group it belongs to.
				path := strings.Split(com[1], "_")
				name := path[len(path)-1]
				list = append(list, fmt.Sprintf("%s- [%s](%s/%s)", strings.Repeat("  ", len(path)), name, directory, entry.Name()))
			} else {
				list = append(list, fmt.Sprintf("- [%s](%s/%s)", com[1], directory, entry.Name()))
			}
		}
	}

	return list, nil
}

func prependTitle(s string) string {
	fmt.Println(s)
	return generatedBanner + "\n\n"
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
		// The note is the phase's own account of why a dry run runs it; only the read-only
		// phases carry one, since they are the ones that touch live hosts.
		item += fmt.Sprintf("\n    - Dry run: %s", phase.ClassifyDryRun(p))
		if note := phase.DryRunNote(p); note != "" {
			item += ". " + note
		}
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
