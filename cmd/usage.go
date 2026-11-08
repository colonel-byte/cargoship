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

package cmd

import (
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// flagGroupOrder is the order the usage sections are printed in, after the ungrouped flags that
// are specific to the command. Build before Registry before Verification follows the order a
// package moves through: it is assembled, then pushed or pulled, then checked.
//
// A title not listed here still renders, sorted after these, so adding a group is one constant and
// one annotateFlagGroup call.
var flagGroupOrder = []string{
	buildFlagGroupTitle,
	registryFlagGroupTitle,
	verifyFlagGroupTitle,
}

// registerUsageTemplate teaches the root command to render the flag-group annotations that
// annotateFlagGroup has always written. Without it every flag landed in one flat alphabetical
// block -- twenty of them on create, which is the command most likely to be read by someone who
// has not run it before.
//
// Cobra inherits the template down the tree, so this is called once on the root.
func registerUsageTemplate(rootCmd *cobra.Command) {
	cobra.AddTemplateFunc("flagSections", flagSections)
	rootCmd.SetUsageTemplate(groupedUsageTemplate)
}

// flagSections renders fs as an ungrouped "Flags:" block followed by one block per group.
//
// Each group is rendered through its own pflag.FlagSet so that pflag does the padding and the
// wrapping, rather than this function reimplementing either. The cost is that column alignment is
// computed per section instead of across the whole set, which reads better anyway: a section of
// short flags is no longer indented to clear the longest flag name on the command.
func flagSections(fs *pflag.FlagSet) string {
	ungrouped := pflag.NewFlagSet("ungrouped", pflag.ContinueOnError)
	grouped := map[string]*pflag.FlagSet{}

	fs.VisitAll(func(f *pflag.Flag) {
		titles := f.Annotations[flagGroupAnnotation]
		if len(titles) == 0 || titles[0] == "" {
			ungrouped.AddFlag(f)
			return
		}
		title := titles[0]
		if grouped[title] == nil {
			grouped[title] = pflag.NewFlagSet(title, pflag.ContinueOnError)
		}
		grouped[title].AddFlag(f)
	})

	var b strings.Builder
	// A command can be entirely grouped flags, in which case there is no "Flags:" heading to
	// print -- writing an empty one would look like a bug.
	if usage := strings.TrimRight(ungrouped.FlagUsages(), "\n"); usage != "" {
		b.WriteString("Flags:\n")
		b.WriteString(usage)
	}

	for _, title := range groupTitlesInOrder(grouped) {
		usage := strings.TrimRight(grouped[title].FlagUsages(), "\n")
		if usage == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(title)
		b.WriteString(":\n")
		b.WriteString(usage)
	}

	return b.String()
}

// groupTitlesInOrder returns the titles present in grouped, those named in flagGroupOrder first and
// in that order, then any others sorted so that the output is stable.
func groupTitlesInOrder(grouped map[string]*pflag.FlagSet) []string {
	out := make([]string, 0, len(grouped))
	seen := make(map[string]bool, len(grouped))

	for _, title := range flagGroupOrder {
		if grouped[title] != nil {
			out = append(out, title)
			seen[title] = true
		}
	}

	rest := make([]string, 0, len(grouped))
	for title := range grouped {
		if !seen[title] {
			rest = append(rest, title)
		}
	}
	sort.Strings(rest)

	return append(out, rest...)
}

// groupedUsageTemplate is cobra's default usage template with one change: the "Flags:" block is
// rendered by flagSections instead of by LocalFlags.FlagUsages, so grouped flags get their own
// headings. Everything else -- the usage line, aliases, examples, the command groups, the global
// flags -- is cobra's, and should be left matching it so that a cobra upgrade is a diff of this
// constant rather than a rewrite.
const groupedUsageTemplate = `Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Additional Commands:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

{{flagSections .LocalFlags | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`
