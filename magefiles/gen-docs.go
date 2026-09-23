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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/cmd"
	"github.com/colonel-byte/cargoship/src/pkg/action"
	"github.com/colonel-byte/cargoship/src/pkg/phase"
	"github.com/colonel-byte/cargoship/src/types/distrocfg"
	goyaml "github.com/goccy/go-yaml"
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
	if err := generateAnsibleDocs(); err != nil {
		return err
	}
	if err := generateBookPages(); err != nil {
		return err
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
	// dryRun adds the dry-run section and per-phase labels to the page. Set it for every
	// action that registers the --dry-run flag, so a page describes the flag exactly when
	// the command accepts it.
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
			dryRun: true,
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
		// lines supplies the section's entries itself, for a section the folder walk below
		// cannot produce. It replaces regex and folder rather than adding to them.
		lines func() ([]string, error)
	}{
		{
			// Both entries are prefix chapters: mdBook gives a line before the first list
			// item no number and places it ahead of every numbered chapter, which is where
			// the front page and the security policy belong.
			title: "Index",
			extra: "[readme](index.md)\n\n[security](security.md)",
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
			title: "Ansible",
			lines: ansibleSummary,
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

// generatedBanner marks a page as generator output, so that an editor who opens one is told
// before they change it that their change will not survive the next commit.
const generatedBanner = "<!-- Page generated by Cargoship; DO NOT EDIT -->"

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

// ---------------------------------------------------------------------------
// Ansible collection reference pages
// ---------------------------------------------------------------------------

// The pages under docs/ansible are generated from the collection itself: a role's interface from
// its meta/argument_specs.yml and defaults/main.yml, a module's from the DOCUMENTATION and
// EXAMPLES blocks in its action plugin. ansible/colonel_byte/cargoship/AGENTS.md is the contract
// these two generators implement.
const (
	// collectionDir is the root of the colonel_byte.cargoship collection.
	collectionDir = "ansible/colonel_byte/cargoship"

	// ansibleDocsDir is where the generated pages land. It is deliberately not in docsDirs:
	// collection.md, roles.md and modules.md are hand-written overviews in the same directory,
	// and the RemoveAll that every docsDirs entry gets would take them with it.
	ansibleDocsDir = "docs/ansible"

	// collectionFQCN is the namespace a playbook writes a module or role under.
	collectionFQCN = "colonel_byte.cargoship"
)

// generatedAnsiblePages are the pages these generators own. They are removed before generation so
// that a deleted module or role does not leave its page behind; everything else in docs/ansible is
// hand-written and untouched.
var generatedAnsiblePages = []string{"module_*.md", "role_*.md"}

var (
	// The DOCUMENTATION and EXAMPLES blocks are pulled out of the plugin by pattern rather than by
	// importing Python, the same way src/internal/ansibleinv/plugin_test.go reads the variable
	// allowlist out of the projection plugin.
	documentationRe = regexp.MustCompile(`(?sm)^DOCUMENTATION = r"""\n(.*?)\n"""$`)
	examplesRe      = regexp.MustCompile(`(?sm)^EXAMPLES = r"""\n(.*?)\n"""$`)
)

// docOption is one documented parameter, from either a module's DOCUMENTATION block or a role's
// argument_specs.yml. The two formats agree on every key used here except cli_flag, which only a
// module has.
type docOption struct {
	Type        string   `yaml:"type"`
	Elements    string   `yaml:"elements"`
	Required    bool     `yaml:"required"`
	Default     any      `yaml:"default"`
	Choices     []string `yaml:"choices"`
	Description []string `yaml:"description"`

	// CLIFlag is the flag the parameter renders on the cargoship command line, or "None" when it
	// renders none. It is a cargoship extension rather than a standard Ansible documentation key;
	// see the comment above DOCUMENTATION in any of the action plugins.
	CLIFlag string `yaml:"cli_flag"`
}

// namedOption is an option and the name it was declared under, kept as a slice rather than a map
// because declaration order is the row order of the generated table.
type namedOption struct {
	name string
	docOption
}

// moduleDoc is a module's DOCUMENTATION block.
type moduleDoc struct {
	Module           string          `yaml:"module"`
	ShortDescription string          `yaml:"short_description"`
	Description      []string        `yaml:"description"`
	VersionAdded     string          `yaml:"version_added"`
	Author           []string        `yaml:"author"`
	Notes            []string        `yaml:"notes"`
	Options          goyaml.MapSlice `yaml:"options"`
}

// roleEntrypoint is one entry point in a role's argument_specs.yml. Only main is documented; a
// role with a second entry point would need a page section of its own.
type roleEntrypoint struct {
	ShortDescription string          `yaml:"short_description"`
	Description      []string        `yaml:"description"`
	Options          goyaml.MapSlice `yaml:"options"`
}

// roleSpecs is a role's meta/argument_specs.yml.
type roleSpecs struct {
	ArgumentSpecs map[string]roleEntrypoint `yaml:"argument_specs"`
}

// generateAnsibleDocs writes the generated reference pages for every role and module in the
// collection.
func generateAnsibleDocs() error {
	if err := os.MkdirAll(ansibleDocsDir, 0o775); err != nil {
		return err
	}
	for _, pattern := range generatedAnsiblePages {
		matches, err := filepath.Glob(filepath.Join(ansibleDocsDir, pattern))
		if err != nil {
			return err
		}
		for _, match := range matches {
			if err := os.Remove(match); err != nil {
				return err
			}
		}
	}

	if err := generateRoleDocs(); err != nil {
		return err
	}
	return generateModuleDocs()
}

// generateRoleDocs writes docs/ansible/role_<role>.md for every role that documents itself.
//
// Discovery is a glob, so a role added later needs no change here -- but a role without an
// argument_specs.yml gets no page, which is why AGENTS.md asks for one first.
func generateRoleDocs() error {
	specs, err := filepath.Glob(filepath.Join(collectionDir, "roles", "*", "meta", "argument_specs.yml"))
	if err != nil {
		return err
	}
	sort.Strings(specs)

	for _, specPath := range specs {
		// roles/<role>/meta/argument_specs.yml
		role := filepath.Base(filepath.Dir(filepath.Dir(specPath)))

		raw, err := os.ReadFile(specPath)
		if err != nil {
			return err
		}
		var parsed roleSpecs
		if err := goyaml.Unmarshal(raw, &parsed); err != nil {
			return fmt.Errorf("reading %s: %w", specPath, err)
		}
		entry, ok := parsed.ArgumentSpecs["main"]
		if !ok {
			return fmt.Errorf("%s: no main entry point", specPath)
		}
		documented, err := orderedOptions(entry.Options)
		if err != nil {
			return fmt.Errorf("reading %s: %w", specPath, err)
		}

		defaultsPath := filepath.Join(collectionDir, "roles", role, "defaults", "main.yml")
		rawDefaults, err := os.ReadFile(defaultsPath)
		if err != nil {
			return err
		}
		var defaults goyaml.MapSlice
		if err := goyaml.Unmarshal(rawDefaults, &defaults); err != nil {
			return fmt.Errorf("reading %s: %w", defaultsPath, err)
		}

		ordered, err := reconcileRoleDefaults(role, documented, defaults)
		if err != nil {
			return err
		}
		if err := writeRoleDoc(role, entry, ordered); err != nil {
			return err
		}
	}
	return nil
}

// reconcileRoleDefaults puts the documented options into defaults/main.yml order, and reports the
// two ways the pair can disagree: a variable with a default and no documentation, or documentation
// and no default. Both would otherwise produce a page that is quietly missing a row.
//
// It also reports a default that the two files spell differently, because then the page would
// document a value the role does not actually use.
func reconcileRoleDefaults(role string, documented []namedOption, defaults goyaml.MapSlice) ([]namedOption, error) {
	byName := make(map[string]namedOption, len(documented))
	for _, o := range documented {
		byName[o.name] = o
	}

	ordered := make([]namedOption, 0, len(documented))
	for _, item := range defaults {
		name, ok := item.Key.(string)
		if !ok {
			return nil, fmt.Errorf("roles/%s/defaults/main.yml: variable name %v is not a string", role, item.Key)
		}
		o, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf(
				"roles/%s: %s has a default but no entry in meta/argument_specs.yml", role, name)
		}
		if spec, actual := renderYAMLValue(o.Default), renderYAMLValue(item.Value); spec != actual {
			return nil, fmt.Errorf(
				"roles/%s: %s defaults to %s in defaults/main.yml but %s in meta/argument_specs.yml",
				role, name, actual, spec)
		}
		// The value the role actually loads is the one that gets documented.
		o.Default = item.Value
		ordered = append(ordered, o)
		delete(byName, name)
	}

	for _, o := range documented {
		if _, unmatched := byName[o.name]; unmatched {
			return nil, fmt.Errorf(
				"roles/%s: %s is documented in meta/argument_specs.yml but has no default in defaults/main.yml",
				role, o.name)
		}
	}
	return ordered, nil
}

// generateModuleDocs writes docs/ansible/module_<action>.md for every action plugin.
func generateModuleDocs() error {
	plugins, err := filepath.Glob(filepath.Join(collectionDir, "plugins", "action", "cargoship_*.py"))
	if err != nil {
		return err
	}
	sort.Strings(plugins)

	for _, pluginPath := range plugins {
		source, err := os.ReadFile(pluginPath)
		if err != nil {
			return err
		}

		block := documentationRe.FindSubmatch(source)
		if block == nil {
			return fmt.Errorf(`%s: no DOCUMENTATION = r""" block`, pluginPath)
		}
		var doc moduleDoc
		if err := goyaml.Unmarshal(block[1], &doc); err != nil {
			return fmt.Errorf("reading the DOCUMENTATION block in %s: %w", pluginPath, err)
		}

		name := strings.TrimSuffix(filepath.Base(pluginPath), ".py")
		if doc.Module != name {
			return fmt.Errorf("%s: documents module %q", pluginPath, doc.Module)
		}
		options, err := orderedOptions(doc.Options)
		if err != nil {
			return fmt.Errorf("reading the DOCUMENTATION block in %s: %w", pluginPath, err)
		}

		examples := examplesRe.FindSubmatch(source)
		if examples == nil {
			return fmt.Errorf(`%s: no EXAMPLES = r""" block`, pluginPath)
		}

		if err := writeModuleDoc(doc, options, string(examples[1])); err != nil {
			return err
		}
	}
	return nil
}

// orderedOptions decodes an options mapping while keeping declaration order, which is the row
// order of the generated table. Unmarshalling straight into a map would lose it.
func orderedOptions(options goyaml.MapSlice) ([]namedOption, error) {
	if len(options) == 0 {
		return nil, errors.New("no options are documented")
	}

	out := make([]namedOption, 0, len(options))
	for _, item := range options {
		name, ok := item.Key.(string)
		if !ok {
			return nil, fmt.Errorf("option name %v is not a string", item.Key)
		}
		// Re-encoded and decoded rather than type-asserted, so the option body is read through
		// the same tags whatever intermediate shape the parser chose for it.
		raw, err := goyaml.Marshal(item.Value)
		if err != nil {
			return nil, fmt.Errorf("re-encoding option %s: %w", name, err)
		}
		var o docOption
		if err := goyaml.Unmarshal(raw, &o); err != nil {
			return nil, fmt.Errorf("decoding option %s: %w", name, err)
		}
		if o.Type == "" {
			return nil, fmt.Errorf("option %s has no type", name)
		}
		if len(o.Description) == 0 {
			return nil, fmt.Errorf("option %s has no description", name)
		}
		out = append(out, namedOption{name: name, docOption: o})
	}
	return out, nil
}

// ansibleMarkup rewrites the semantic markup Ansible's documentation format uses into the Markdown
// the book renders. Only the four forms the collection writes are handled; an unhandled one comes
// through verbatim, which reads as a bug rather than disappearing silently.
var ansibleMarkup = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`\bC\(([^()]*)\)`), "`$1`"},
	{regexp.MustCompile(`\bM\(([^()]*)\)`), "`$1`"},
	{regexp.MustCompile(`\bB\(([^()]*)\)`), "**$1**"},
	{regexp.MustCompile(`\bI\(([^()]*)\)`), "*$1*"},
}

// renderProse converts one documentation string into Markdown.
func renderProse(text string) string {
	for _, m := range ansibleMarkup {
		text = m.re.ReplaceAllString(text, m.repl)
	}
	return text
}

// renderDescription joins an option's description into one table cell. The lines are sentences of
// one paragraph rather than separate paragraphs, which is what a cell can hold.
func renderDescription(o namedOption) string {
	parts := make([]string, 0, len(o.Description)+1)
	for _, line := range o.Description {
		parts = append(parts, renderProse(line))
	}
	if len(o.Choices) > 0 {
		quoted := make([]string, 0, len(o.Choices))
		for _, choice := range o.Choices {
			quoted = append(quoted, "`"+choice+"`")
		}
		parts = append(parts, fmt.Sprintf("One of %s.", strings.Join(quoted, ", ")))
	}
	return strings.Join(parts, " ")
}

// renderType renders an option's type, naming the element type of a list so that a reader knows
// what goes in it.
func renderType(o namedOption) string {
	if o.Elements != "" {
		return fmt.Sprintf("`%s` of `%s`", o.Type, o.Elements)
	}
	return "`" + o.Type + "`"
}

// renderYAMLValue renders a default value the way it is written in YAML, so that an empty string
// reads as "" rather than as an empty cell that could mean anything. A non-empty string is left
// unquoted, because the cell puts it in a code span already.
func renderYAMLValue(value any) string {
	if value == nil {
		return "null"
	}
	if s, ok := value.(string); ok {
		if s == "" {
			return `""`
		}
		return s
	}
	out, err := goyaml.Marshal(value)
	if err != nil {
		// A value that came out of a YAML document goes back into one. Reporting the failure in
		// the cell is better than failing the whole run over a default.
		return fmt.Sprintf("(unrenderable: %v)", err)
	}
	return strings.TrimSpace(string(out))
}

// renderCLIFlag renders the flag an option maps onto. "None" is the documented spelling for an
// option that renders no flag, and it is left as prose rather than dressed up as code, because
// there is no flag there to quote.
func renderCLIFlag(o namedOption) string {
	if o.CLIFlag == "" || o.CLIFlag == "None" {
		return "None"
	}
	return "`" + o.CLIFlag + "`"
}

// paddedTable renders a table in the aligned form ansible/AGENTS.md requires: every cell padded to
// the width of the widest cell in its column, one space of padding inside each pipe, and the
// delimiter row's dashes run out to that same width, so every line of the table is the same length.
//
// Neither writer in the markdown package produces that form. Table writes a fixed-width delimiter
// row and pads nothing at all. CustomTable pads, through tablewriter, but centres the header cells
// and writes the delimiter row with no space inside its pipes. Rendering the rows here is shorter
// than correcting either, and it does not change shape when tablewriter's formatting does.
func paddedTable(header []string, rows [][]string) []string {
	// Cells hold prose, which may contain a pipe or a line break; both have to be escaped before
	// their width is measured, or the padding is computed against the wrong string.
	escaped := make([][]string, 0, len(rows)+1)
	width := make([]int, len(header))
	for _, row := range append([][]string{header}, rows...) {
		cells := make([]string, len(header))
		for i := range cells {
			if i < len(row) {
				cells[i] = markdown.EscapeTableCell(row[i])
			}
			if n := utf8.RuneCountInString(cells[i]); n > width[i] {
				width[i] = n
			}
		}
		escaped = append(escaped, cells)
	}

	line := func(cells []string) string {
		var b strings.Builder
		b.WriteString("|")
		for i, cell := range cells {
			b.WriteString(" ")
			b.WriteString(cell)
			b.WriteString(strings.Repeat(" ", width[i]-utf8.RuneCountInString(cell)))
			b.WriteString(" |")
		}
		return b.String()
	}

	delimiter := make([]string, len(header))
	for i := range delimiter {
		delimiter[i] = strings.Repeat("-", width[i])
	}

	out := make([]string, 0, len(escaped)+1)
	out = append(out, line(escaped[0]), line(delimiter))
	for _, row := range escaped[1:] {
		out = append(out, line(row))
	}
	return out
}

// writeGeneratedPage opens a page under docs/ansible and hands it to write, with the banner that
// says the page is generated already in place.
func writeGeneratedPage(name string, write func(doc *markdown.Markdown) error) error {
	path := filepath.Join(ansibleDocsDir, name)
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
	doc.PlainText(generatedBanner)
	doc.PlainText("")

	if err := write(doc); err != nil {
		return err
	}
	return doc.Build()
}

// writeModuleDoc renders docs/ansible/module_<action>.md.
func writeModuleDoc(doc moduleDoc, options []namedOption, examples string) error {
	// The page is named after the action rather than the module, so that module_apply.md sits
	// beside role_cluster.md without stuttering cargoship_ through every filename.
	action := strings.TrimPrefix(doc.Module, "cargoship_")

	return writeGeneratedPage(fmt.Sprintf("module_%s.md", action), func(md *markdown.Markdown) error {
		md.H2(doc.Module)
		md.PlainText("")
		md.PlainText(renderProse(doc.ShortDescription) + ".")
		md.PlainText("")
		md.PlainTextf("Write it as `%s.%s`. It runs on the management node; nothing is installed on the fleet.", collectionFQCN, doc.Module)
		md.PlainText("")
		for _, line := range doc.Description {
			md.PlainText(renderProse(line))
			md.PlainText("")
		}

		md.H3("Parameters")
		md.PlainText("")
		rows := make([][]string, 0, len(options))
		for _, o := range options {
			required := "no"
			if o.Required {
				required = "yes"
			}
			rows = append(rows, []string{
				"`" + o.name + "`",
				renderType(o),
				required,
				renderCLIFlag(o),
				renderDescription(o),
			})
		}
		for _, line := range paddedTable([]string{"Parameter", "Type", "Required", "Flag", "Description"}, rows) {
			md.PlainText(line)
		}
		md.PlainText("")

		if len(doc.Notes) > 0 {
			md.H3("Notes")
			md.PlainText("")
			notes := make([]string, 0, len(doc.Notes))
			for _, note := range doc.Notes {
				notes = append(notes, renderProse(note))
			}
			md.BulletList(notes...)
			md.PlainText("")
		}

		md.H3("Example")
		md.PlainText("")
		md.CodeBlocks(markdown.SyntaxHighlightYAML, strings.TrimSpace(examples))
		md.PlainText("")
		return nil
	})
}

// writeRoleDoc renders docs/ansible/role_<role>.md.
func writeRoleDoc(role string, entry roleEntrypoint, options []namedOption) error {
	return writeGeneratedPage(fmt.Sprintf("role_%s.md", role), func(md *markdown.Markdown) error {
		md.H2(role)
		md.PlainText("")
		md.PlainText(renderProse(entry.ShortDescription) + ".")
		md.PlainText("")
		md.PlainTextf("Include it as `%s.%s`.", collectionFQCN, role)
		md.PlainText("")
		for _, line := range entry.Description {
			md.PlainText(renderProse(line))
			md.PlainText("")
		}

		md.H3("Variables")
		md.PlainText("")
		rows := make([][]string, 0, len(options))
		for _, o := range options {
			rows = append(rows, []string{
				"`" + o.name + "`",
				renderType(o),
				"`" + renderYAMLValue(o.Default) + "`",
				renderDescription(o),
			})
		}
		for _, line := range paddedTable([]string{"Variable", "Type", "Default", "Description"}, rows) {
			md.PlainText(line)
		}
		md.PlainText("")
		return nil
	})
}

// ansibleSummary builds the entries of the Ansible chapter, nesting each generated page under the
// overview that introduces it.
//
// The generic folder walk cannot produce this. docs/ansible holds three hand-written overviews
// alongside the generated pages, and read in name order modules.md and roles.md would both land
// after the pages they introduce, in one flat list that says nothing about which is which.
func ansibleSummary() ([]string, error) {
	lines := []string{
		"- [collection](ansible/collection.md)",
	}

	// Globbed rather than listed, so a module or role added later appears here on its own.
	for _, section := range []struct {
		overview string
		pattern  string
		prefix   string
	}{
		{overview: "modules", pattern: "module_*.md", prefix: "module_"},
		{overview: "roles", pattern: "role_*.md", prefix: "role_"},
	} {
		lines = append(lines, fmt.Sprintf("- [%s](ansible/%s.md)", section.overview, section.overview))

		pages, err := filepath.Glob(filepath.Join(ansibleDocsDir, section.pattern))
		if err != nil {
			return nil, err
		}
		sort.Strings(pages)
		for _, page := range pages {
			name := filepath.Base(page)
			label := strings.TrimSuffix(strings.TrimPrefix(name, section.prefix), ".md")
			lines = append(lines, fmt.Sprintf("  - [%s](ansible/%s)", label, name))
		}
	}

	return lines, nil
}

// Two pages of the book are written outside docs/, because a reader other than mdBook looks for
// them where they are: GitHub renders README.md as the repository's front page, and
// .github/SECURITY.md as its security policy, from the fixed paths it expects. Each is copied into
// docs/ rather than symlinked, because the two readers resolve a relative link against different
// directories -- GitHub against the file's own directory in the repository, mdBook against docs/,
// since book.toml sets src = "docs" and the page is served from the site root. No single relative
// path satisfies both.
var bookPages = []struct {
	// source is the hand-written file. It keeps links relative to its own directory in the
	// repository, which is what GitHub and an editor want.
	source string

	// page is the generated copy under docs/, with its links rewritten for the book.
	page string
}{
	{source: "README.md", page: "docs/index.md"},
	{source: ".github/SECURITY.md", page: "docs/security.md"},
}

// docsDir is the book's source directory, and the only part of the repository a link on a
// generated page can reach.
const docsDir = "docs"

// markdownLink matches the target of a Markdown inline link or image, with its optional title.
// Reference-style links and autolinks are not matched, and neither source uses either.
var markdownLink = regexp.MustCompile(`(!?\]\()([^)\s]+)((?:\s+"[^"]*")?\))`)

// generateBookPages copies each file in bookPages into the book, rewriting the links that would
// otherwise break there.
func generateBookPages() error {
	for _, p := range bookPages {
		if err := generateBookPage(p.source, p.page); err != nil {
			return err
		}
	}
	return nil
}

// generateBookPage writes one page of the book from a source outside docs/.
//
// A link into docs/ is rewritten relative to the generated page. Anything else relative fails the
// run naming the target, since the alternative is publishing a link that 404s on a page nobody
// thinks of as generated. Absolute URLs, anchors, and the README's badge images pass through
// untouched.
func generateBookPage(source, page string) error {
	content, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("unable to read %s: %w", source, err)
	}

	sourceDir, pageDir := filepath.Dir(source), filepath.Dir(page)

	var unresolved []string
	rewritten := markdownLink.ReplaceAllStringFunc(string(content), func(match string) string {
		parts := markdownLink.FindStringSubmatch(match)
		open, target, tail := parts[1], parts[2], parts[3]

		rebased, ok := rebaseLink(sourceDir, pageDir, target)
		if !ok {
			unresolved = append(unresolved, target)
			return match
		}
		return open + rebased + tail
	})

	if len(unresolved) > 0 {
		sort.Strings(unresolved)
		return fmt.Errorf(
			"%s links to %s, which will not resolve in %s: the book is built from %s/, so a relative link has to point at a file inside it. Move the target under %s/, or write the link as an absolute URL",
			source, strings.Join(unresolved, ", "), page, docsDir, docsDir,
		)
	}

	// A page that was a symlink in an earlier tree is still one here, and writing through it
	// would write to the source instead.
	if err := os.Remove(page); err != nil && !os.IsNotExist(err) {
		return err
	}

	fmt.Println(page)
	return os.WriteFile(page, []byte(generatedBanner+"\n\n"+rewritten), 0o644)
}

// rebaseLink rewrites one link target from the source page's directory to the generated page's,
// and reports whether the result resolves.
//
// A target that is not a relative path into the repository -- an absolute URL, a protocol-relative
// one, a bare fragment -- is left exactly as it is and reported fine, because its meaning does not
// depend on which directory the page sits in.
func rebaseLink(sourceDir, pageDir, target string) (string, bool) {
	switch {
	case strings.HasPrefix(target, "#"):
		// A fragment is resolved against the page itself either way.
		return target, true
	case strings.Contains(target, "://"), strings.HasPrefix(target, "//"), strings.HasPrefix(target, "mailto:"):
		// A scheme, or a protocol-relative URL, is not a path in this repository.
		return target, true
	case target == "", strings.HasPrefix(target, "/"):
		// An empty target, and one rooted at the server, resolve to neither base.
		return target, false
	}

	// The fragment travels with the link but is not part of the path being checked.
	path, fragment := target, ""
	if i := strings.Index(path, "#"); i >= 0 {
		path, fragment = path[:i], path[i:]
	}

	// What the link means on GitHub: a path relative to the repository root. A target that
	// climbs out of the repository lands outside docs/ and is refused below with the rest.
	repoPath := filepath.Join(sourceDir, path)
	if repoPath != docsDir && !strings.HasPrefix(repoPath, docsDir+string(filepath.Separator)) {
		return target, false
	}

	// mdBook rewrites a .md target to .html itself, so the extension is left alone and the file
	// is what gets checked.
	if _, err := os.Stat(repoPath); err != nil {
		return target, false
	}

	rebased, err := filepath.Rel(pageDir, repoPath)
	if err != nil {
		return target, false
	}
	return rebased + fragment, true
}
