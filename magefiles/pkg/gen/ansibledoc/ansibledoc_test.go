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

package ansibledoc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/magefiles/pkg/repo"
	goyaml "github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

// mapSlice builds an options mapping the way the YAML parser hands one over, so a test can state
// declaration order explicitly.
func mapSlice(pairs ...any) goyaml.MapSlice {
	out := make(goyaml.MapSlice, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, goyaml.MapItem{Key: pairs[i], Value: pairs[i+1]})
	}
	return out
}

// option is the body of one documented option, as a map so orderedOptions decodes it through the
// same path a parsed file takes.
func option(extra ...string) map[string]any {
	o := map[string]any{"type": "str", "description": []any{"Something."}}
	for i := 0; i < len(extra); i += 2 {
		o[extra[i]] = extra[i+1]
	}
	return o
}

func TestOrderedOptionsKeepsDeclarationOrder(t *testing.T) {
	got, err := orderedOptions(mapSlice(
		"zulu", option(),
		"alpha", option("type", "bool"),
		"mike", option("elements", "str", "type", "list"),
	))
	require.NoError(t, err)

	names := make([]string, 0, len(got))
	for _, o := range got {
		names = append(names, o.name)
	}
	require.Equal(t, []string{"zulu", "alpha", "mike"}, names,
		"declaration order is the row order of the generated table")

	require.Equal(t, "bool", got[1].Type)
	require.Equal(t, "str", got[2].Elements)
}

func TestOrderedOptionsRejects(t *testing.T) {
	for name, tc := range map[string]struct {
		options goyaml.MapSlice
		wants   string
	}{
		"nothing documented": {
			options: nil,
			wants:   "no options are documented",
		},
		"a non-string name": {
			options: mapSlice(7, option()),
			wants:   "is not a string",
		},
		"a missing type": {
			options: mapSlice("nada", map[string]any{"description": []any{"Something."}}),
			wants:   "option nada has no type",
		},
		"a missing description": {
			options: mapSlice("nada", map[string]any{"type": "str"}),
			wants:   "option nada has no description",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := orderedOptions(tc.options)
			require.ErrorContains(t, err, tc.wants)
		})
	}
}

func TestReconcileRoleDefaultsUsesDefaultsOrder(t *testing.T) {
	documented := []namedOption{
		{name: "beta", docOption: docOption{Type: "str", Default: "b"}},
		{name: "alpha", docOption: docOption{Type: "str", Default: "a"}},
	}
	ordered, err := reconcileRoleDefaults("demo", documented, mapSlice("alpha", "a", "beta", "b"))
	require.NoError(t, err)
	require.Equal(t, []string{"alpha", "beta"}, []string{ordered[0].name, ordered[1].name})

	// The value the role actually loads is what gets documented, so it comes from defaults.
	require.Equal(t, "a", ordered[0].Default)
}

func TestReconcileRoleDefaultsReportsDisagreement(t *testing.T) {
	documented := []namedOption{{name: "alpha", docOption: docOption{Type: "str", Default: "a"}}}

	for name, tc := range map[string]struct {
		documented []namedOption
		defaults   goyaml.MapSlice
		wants      string
	}{
		"a default with no documentation": {
			documented: documented,
			defaults:   mapSlice("alpha", "a", "ghost", "g"),
			wants:      "ghost has a default but no entry in meta/argument_specs.yml",
		},
		"documentation with no default": {
			documented: documented,
			defaults:   nil,
			wants:      "alpha is documented in meta/argument_specs.yml but has no default",
		},
		"two spellings of the default": {
			documented: documented,
			defaults:   mapSlice("alpha", "z"),
			wants:      "alpha defaults to z in defaults/main.yml but a in meta/argument_specs.yml",
		},
		"a non-string variable name": {
			documented: documented,
			defaults:   mapSlice(7, "a"),
			wants:      "variable name 7 is not a string",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := reconcileRoleDefaults("demo", tc.documented, tc.defaults)
			require.ErrorContains(t, err, tc.wants)
		})
	}
}

// TestSummaryMatchesTheCheckedInPages checks the one thing the generic folder walk could not do:
// each overview comes before the pages it introduces, and those are indented under it.
func TestSummaryMatchesTheCheckedInPages(t *testing.T) {
	repo.Chdir(t)

	lines, err := Summary()
	require.NoError(t, err)
	require.Equal(t, "- [collection](ansible/collection.md)", lines[0])

	modules := indexOf(t, lines, "- [modules](ansible/modules.md)")
	roles := indexOf(t, lines, "- [roles](ansible/roles.md)")
	require.Less(t, modules, roles)

	for i, line := range lines {
		if !strings.HasPrefix(line, "  - [") {
			continue
		}
		switch {
		case i > roles:
			require.Contains(t, line, "](ansible/role_")
		default:
			require.Greater(t, i, modules, "a nested entry cannot precede its overview")
			require.Contains(t, line, "](ansible/module_")
		}
	}

	// Every page the sections glob has to exist, since SUMMARY.md links to it.
	for _, line := range lines {
		target := line[strings.Index(line, "](")+2 : len(line)-1]
		_, err := os.Stat(filepath.Join("docs", target))
		require.NoError(t, err)
	}
}

func indexOf(t *testing.T, lines []string, want string) int {
	t.Helper()
	for i, line := range lines {
		if line == want {
			return i
		}
	}
	require.FailNowf(t, "missing entry", "%q is not in %v", want, lines)
	return -1
}
