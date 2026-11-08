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

package ansibleinv

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// hostVarsJSONNames returns the variable names HostVars declares, which are its json tags.
func hostVarsJSONNames(t *testing.T) []string {
	t.Helper()
	typ := reflect.TypeOf(HostVars{})
	names := make([]string, 0, typ.NumField())
	for i := range typ.NumField() {
		tag := typ.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		require.NotEmpty(t, name, "field %s has no json tag", typ.Field(i).Name)
		names = append(names, name)
	}
	return names
}

// TestHostVarsCoverKnownVariables pins HostVars to the variables hostFromVars actually reads.
//
// The symptom of drift is silence in both directions: a variable added to `known` and not to
// HostVars is one an editor reports as not allowed while cargoship reads it happily, and a field
// in HostVars with no entry in `known` is one an editor offers and `checkUnknown` then refuses.
// Neither fails anything else, which is why this exists.
func TestHostVarsCoverKnownVariables(t *testing.T) {
	declared := map[string]bool{}
	for _, name := range hostVarsJSONNames(t) {
		if strings.HasPrefix(name, Prefix) {
			declared[name] = true
		}
	}

	expected := map[string]bool{}
	for _, name := range known {
		expected[name] = true
	}

	require.Equal(t, expected, declared,
		"HostVars and the known list in vars.go describe different variables")
}

// TestHostVarsCoverAnsibleVariables pins the four ansible_ variables the same way.
func TestHostVarsCoverAnsibleVariables(t *testing.T) {
	declared := map[string]bool{}
	for _, name := range hostVarsJSONNames(t) {
		if !strings.HasPrefix(name, Prefix) {
			declared[name] = true
		}
	}

	require.Equal(t, map[string]bool{
		VarAnsibleHost:    true,
		VarAnsiblePort:    true,
		VarAnsibleUser:    true,
		VarAnsibleKeyFile: true,
	}, declared, "HostVars and the ansible_ constants in vars.go describe different variables")
}

// TestUnknownPrefixedPatternMatchesOnlyUnknowns checks the lookahead the generated schema rejects
// unknown variables with, in Go's regexp dialect's nearest equivalent -- Go's RE2 has no
// lookahead, so the pattern itself cannot be compiled here. What is checked instead is that the
// pattern names every known variable, which is the part that can go wrong silently; that the
// lookahead is honoured at all was verified against yaml-language-server itself.
func TestUnknownPrefixedPatternMatchesOnlyUnknowns(t *testing.T) {
	pattern := unknownPrefixedPattern()

	require.True(t, strings.HasPrefix(pattern, "^"+Prefix+"(?!"))
	require.True(t, strings.HasSuffix(pattern, ")"))

	for _, name := range known {
		alternative := strings.TrimPrefix(name, Prefix) + "$"
		require.Contains(t, pattern, alternative,
			"the schema's reject pattern does not exempt %s, so an editor would flag a variable cargoship reads", name)
	}

	// Every alternative is anchored. One that was not would exempt far more than its own name:
	// a bare `profile` would also exempt `profilexyz`.
	alternatives := regexp.MustCompile(`\(\?!(.*)\)$`).FindStringSubmatch(pattern)
	require.Len(t, alternatives, 2)
	for _, alternative := range strings.Split(alternatives[1], "|") {
		require.True(t, strings.HasSuffix(alternative, "$"),
			"alternative %q is unanchored", alternative)
	}
}
