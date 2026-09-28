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

// Package phasedoc renders docs/phases/<name>.md: one page per action, listing the phases it runs
// in order with each phase's own title and explanation, and -- for an action that takes --dry-run
// -- what a dry run does with each of them.
//
// The pages are built from the actions themselves rather than from a description of them, so a
// phase added to an action appears on its page without anything here changing.
package phasedoc

import (
	"fmt"
	"os"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/pkg/action"
	"github.com/colonel-byte/cargoship/pkg/phase"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/nao1215/markdown"
)

// Dir is where Write puts its pages, relative to the repository root.
const Dir = "docs/phases"

// Manager building blocks, reused across the Docs() entries below.
var (
	genDocsManager = &phase.Manager{
		DistroID: distrocfg.DistroRKE2,
		Config: &cluster.ZarfCluster{
			Metadata: cluster.ZarfClusterMetadata{Name: "gen-docs"},
		},
	}
	genDocsManagerNoConfig = &phase.Manager{DistroID: distrocfg.DistroRKE2}
)

// Doc pairs a docs/phases/<name>.md file with the phase list to render into it.
type Doc struct {
	Name   string
	Phases phase.Phases
	// DryRun adds the dry-run section and per-phase labels to the page. Set it for every
	// action that registers the --dry-run flag, so a page describes the flag exactly when
	// the command accepts it.
	DryRun bool
}

// Docs lists every action whose phases get a docs/phases/<name>.md page. Add a new action's
// phases here to get it picked up by Generate.Document -- no new function needed.
func Docs() []Doc {
	return []Doc{
		{
			Name: "apply",
			Phases: action.NewApply(action.ApplyOptions{
				Manager: genDocsManager,
			}).Phases,
			DryRun: true,
		},
		{
			Name: "reset",
			Phases: action.NewReset(action.ResetOptions{
				Manager: genDocsManagerNoConfig,
			}).Phases,
			DryRun: true,
		},
		{
			Name: "kube-config",
			Phases: action.NewKubeConfig(action.KubeConfigOptions{
				Manager: genDocsManager,
			}).Phases,
		},
		{
			Name:   "prepare",
			Phases: action.NewPrepare(action.PrepareOptions{}).Phases,
			DryRun: true,
		},
		{
			Name: "engine-config-sync",
			Phases: action.NewEngineConfigSync(action.EngineConfigSyncOptions{
				Manager: genDocsManager,
			}).Phases,
			DryRun: true,
		},
	}
}

// phaseComment writes one phase as a list item.
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

// Write renders one docs/phases/<name>.md page listing each phase's title and explanation.
func Write(pd Doc) error {
	path := fmt.Sprintf("%s/%s.md", Dir, pd.Name)
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
	doc.H2(fmt.Sprintf("%s phases", pd.Name))

	if pd.DryRun {
		doc.PlainText(dryRunNote)
		doc.PlainTextf("")
	}

	for _, p := range pd.Phases {
		phaseComment(doc, p, pd.DryRun)
	}

	doc.PlainTextf("")

	return doc.Build()
}

// Generate renders a page for every entry in Docs.
func Generate() error {
	for _, pd := range Docs() {
		if err := Write(pd); err != nil {
			return err
		}
	}
	return nil
}
