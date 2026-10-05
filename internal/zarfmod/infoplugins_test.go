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

package zarfmod

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// infoModules are the read-only modules, whose whole implementation is the Python action plugin
// rather than a wrapper binary driven from this package. docs/agent/choice-zarf-info-modules.md is
// the record of why, and of what this file can and cannot check as a result.
var infoModules = []string{
	"zarf_package_info",
	"zarf_package_inspect",
	"zarf_state_info",
}

// commandPartsBody finds the body of a plugin's command_parts function: everything from its def
// line up to the next line that starts a new top-level statement. Python has no brace to match on,
// so the end of the body is the first line at column 0 that is not blank.
var commandPartsBody = regexp.MustCompile(`(?sm)^def command_parts\(params\):\n(.*?)(?:\n[^\s#]|\z)`)

// renderedFlagLiteral is a flag spelled as a string literal inside command_parts. Every flag a
// read-only module renders is written there and nowhere else, which is the whole reason the
// function exists as a plain function of the parameters.
//
// The second group is the "=value" of a flag written as one word. It is optional because most
// flags are not, and it is captured because for some of them it is the whole point: see
// equalsFlags.
var renderedFlagLiteral = regexp.MustCompile(`"(--[a-z][a-z0-9-]*)(=[^"]*)?"`)

// equalsFlags are the flags that must be written as one word, --name=value, rather than as two.
// Each one declares a no-argument default, so pflag reads it the way it reads a boolean: the space
// form leaves the value as a positional argument and zarf rejects the command for having one too
// many. internal/zarfmod renders these through command.valueFlag, and
// docs/agent/choice-zarf-ansible-module.md records the finding.
//
// This is the one thing TestInfoPluginDocsMatchRenderedFlags can say about how a value attaches,
// and it is here because it is the mistake worth catching: a module that renders --verify always
// fails against every package rather than silently, but it fails at run time on a cluster rather
// than here.
var equalsFlags = map[string]bool{
	"verify": true,
}

// infoOwnFlags are the flags a read-only module renders for itself rather than for any parameter,
// so that the every-rendered-flag-is-documented direction below has to let them through. It is
// moduleOwnFlags plus one: --output-format is how zarf is asked for the JSON the module parses, and
// no parameter may control it, because a module that returned a table would return nothing a
// playbook could read.
var infoOwnFlags = map[string]bool{
	"output-format": true,
}

// TestEveryActionPluginIsCovered is what stops a module being added with no contract test at all.
//
// A module in this collection is written one of two ways: as a wrapper binary whose parameters are
// a Go struct, which TestActionPluginDocsMatchModuleParams holds against its DOCUMENTATION block,
// or as a read-only action plugin, which TestInfoPluginDocsMatchRenderedFlags holds against its
// command_parts. Both tests read a list, and a plugin on neither list is checked by neither -- the
// state the three read-only plugins were first added in. This test fails on that state rather than
// leaving it to be noticed.
func TestEveryActionPluginIsCovered(t *testing.T) {
	plugins, err := filepath.Glob(filepath.Join(collectionRoot, "plugins", "action", "*.py"))
	if err != nil {
		t.Fatalf("unable to list the action plugins: %v", err)
	}
	if len(plugins) == 0 {
		t.Fatalf("no action plugins found under %s, so this test proves nothing", collectionRoot)
	}

	covered := map[string]string{}
	for _, tt := range wrapperModuleCases() {
		covered[tt.module] = "wrapperModuleCases in plugindocs_test.go"
	}
	for _, module := range infoModules {
		if where, already := covered[module]; already {
			t.Errorf("%s is listed in both infoModules and %s", module, where)
			continue
		}
		covered[module] = "infoModules in this file"
	}

	for _, path := range plugins {
		module := strings.TrimSuffix(filepath.Base(path), ".py")
		if _, ok := covered[module]; ok {
			continue
		}
		t.Errorf("the action plugin %s is in neither wrapperModuleCases nor infoModules, so no "+
			"test holds its documented interface against the one it has. Add it to whichever "+
			"list matches how it is built.", module)
	}

	// The other direction: a list naming a plugin that no longer exists is a test that passes by
	// checking nothing.
	onDisk := map[string]bool{}
	for _, path := range plugins {
		onDisk[strings.TrimSuffix(filepath.Base(path), ".py")] = true
	}
	for module, where := range covered {
		if !onDisk[module] {
			t.Errorf("%s names %s, which is not a plugin under plugins/action", where, module)
		}
	}
}

// TestInfoPluginDocsMatchRenderedFlags holds a read-only module's documented cli_flag values
// against the flags its command_parts renders, in both directions.
//
// It is the weaker half of what TestActionPluginDocsMatchModuleParams does for the wrapper
// modules, and the difference is worth stating. That test populates a Go struct, renders a real
// command line, and compares the flags that came out. There is no Go struct here, so this test
// reads flag spellings out of the Python source as string literals. What follows:
//
//   - it sees which flags are named, not whether they are rendered for the right parameter, nor in
//     what order, nor whether a value is attached with a space or an "=". --verify=always, the
//     spelling docs/agent/choice-zarf-ansible-module.md records as load-bearing, is exactly the
//     kind of mistake this cannot catch.
//   - it depends on every flag being a literal inside command_parts. A flag assembled from a
//     variable is invisible to it, which is why the plugins spell each one out.
//
// Both are acceptable because these modules render at most one flag that any parameter controls.
// Should that stop being true, the module wants a wrapper binary rather than a better regexp.
func TestInfoPluginDocsMatchRenderedFlags(t *testing.T) {
	for _, module := range infoModules {
		t.Run(module, func(t *testing.T) {
			documented := documentedOptions(t, module)
			rendered := renderedFlagLiterals(t, module)

			t.Run("every documented flag is rendered", func(t *testing.T) {
				for name, option := range documented {
					if option.CLIFlag == "" || option.CLIFlag == "None" {
						// "None", the documented spelling for a parameter that renders no flag.
						// An absent key means the same thing: the generator renders both as None.
						continue
					}
					if !contains(rendered, strings.TrimPrefix(option.CLIFlag, "--")) {
						t.Errorf("parameter %s documents cli_flag %q, which command_parts does not name",
							name, option.CLIFlag)
					}
				}
			})

			t.Run("every rendered flag is documented", func(t *testing.T) {
				flags := map[string]bool{}
				for _, option := range documented {
					if flag, renders := strings.CutPrefix(option.CLIFlag, "--"); renders {
						flags[flag] = true
					}
				}
				for _, flag := range rendered {
					if moduleOwnFlags[flag] || infoOwnFlags[flag] {
						continue
					}
					if !flags[flag] {
						t.Errorf("command_parts names --%s, which no documented parameter names", flag)
					}
				}
			})
		})
	}
}

// renderedFlagLiterals returns the flag names spelled inside one plugin's command_parts, sorted and
// without duplicates and without the leading dashes, so that it compares against renderedFlags.
func renderedFlagLiterals(t *testing.T, module string) []string {
	t.Helper()

	path := filepath.Join(collectionRoot, "plugins", "action", module+".py")
	source, err := os.ReadFile(path) //nolint:gosec // a path assembled from this test's own constants
	if err != nil {
		t.Fatalf("unable to read %s: %v", path, err)
	}

	body := commandPartsBody.FindSubmatch(source)
	if body == nil {
		t.Fatalf("%s declares no command_parts(params) function, which is where a read-only "+
			"module names the flags it renders", path)
	}

	seen := map[string]bool{}
	for _, match := range renderedFlagLiteral.FindAllSubmatch(body[1], -1) {
		name := strings.TrimPrefix(string(match[1]), "--")
		if equalsFlags[name] && len(match[2]) == 0 {
			t.Errorf("command_parts in %s renders --%s as a separate word. It declares a "+
				"no-argument default, so it has to be written --%s=<value>; see equalsFlags.",
				path, name, name)
		}
		seen[name] = true
	}
	return sortedKeys(seen)
}
