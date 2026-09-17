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

package assemble

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	zarf "github.com/zarf-dev/zarf/src/api/v1alpha1"
	"github.com/zarf-dev/zarf/src/pkg/value"
)

// templated is the opt-in flag an action carries to be rendered at all.
func templated() *bool {
	b := true
	return &b
}

// runActionWriting runs one action whose command writes to out.txt in dir, and
// returns what the command wrote.
func runActionWriting(t *testing.T, dir string, action zarf.ZarfComponentAction, values value.Values) (string, error) {
	t.Helper()
	ctx := context.Background()
	err := runCreateActions(ctx, dir, zarf.ZarfComponentActionDefaults{}, []zarf.ZarfComponentAction{action}, values, newVariableConfig(ctx))
	if err != nil {
		return "", err
	}
	out, readErr := os.ReadFile(filepath.Join(dir, "out.txt"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	return strings.TrimSpace(string(out)), nil
}

func TestRunCreateActionsRendersValues(t *testing.T) {
	dir := t.TempDir()
	action := zarf.ZarfComponentAction{
		Template: templated(),
		Cmd:      `echo {{ .Values.greeting }} > out.txt`,
	}

	got, err := runActionWriting(t, dir, action, value.Values{"greeting": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello" {
		t.Fatalf("got %q, want %q", got, "hello")
	}
}

func TestRunCreateActionsLeavesUnflaggedActionsAlone(t *testing.T) {
	dir := t.TempDir()
	action := zarf.ZarfComponentAction{
		Cmd: `echo '{{ .Values.greeting }}' > out.txt`,
	}

	got, err := runActionWriting(t, dir, action, value.Values{"greeting": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "{{ .Values.greeting }}" {
		t.Fatalf("got %q, want the command to be run verbatim", got)
	}
}

// The create funcs are the reason cargoship renders actions itself rather than
// letting Zarf do it: Zarf's function map cannot be added to from outside.
func TestRunCreateActionsHasCreateFuncs(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "present.txt")
	if err := os.WriteFile(present, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	action := zarf.ZarfComponentAction{
		Template: templated(),
		Cmd:      `echo {{ fileExists "` + present + `" }} {{ fileExists "` + filepath.Join(dir, "absent.txt") + `" }} > out.txt`,
	}

	got, err := runActionWriting(t, dir, action, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "true false" {
		t.Fatalf("got %q, want %q", got, "true false")
	}
}

// toYamlPretty is one of the five functions Zarf's action templating has and
// sprout's sprig layer does not, so an action moved from Zarf keeps working.
func TestRunCreateActionsHasHelmExtras(t *testing.T) {
	dir := t.TempDir()
	action := zarf.ZarfComponentAction{
		Template: templated(),
		Cmd:      `echo '{{ toYamlPretty .Values.registry }}' > out.txt`,
	}

	got, err := runActionWriting(t, dir, action, value.Values{"registry": map[string]any{"address": "127.0.0.1:31999"}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "address: 127.0.0.1:31999" {
		t.Fatalf("got %q, want the value rendered as yaml", got)
	}
}

// A variable one action sets has to reach the action after it, which is why the
// variable config is built once for the whole set rather than per call.
func TestRunCreateActionsSharesVariablesBetweenActions(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	list := []zarf.ZarfComponentAction{
		{
			Cmd:          `echo from-the-first-action`,
			SetVariables: []zarf.Variable{{Name: "FIRST"}},
		},
		{
			Template: templated(),
			Cmd:      `echo {{ .Variables.FIRST }} > out.txt`,
		},
	}

	if err := runCreateActions(ctx, dir, zarf.ZarfComponentActionDefaults{}, list, nil, newVariableConfig(ctx)); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != "from-the-first-action" {
		t.Fatalf("got %q, want the variable the first action set", got)
	}
}

func TestRunCreateActionsReportsATemplateError(t *testing.T) {
	dir := t.TempDir()
	action := zarf.ZarfComponentAction{
		Template:    templated(),
		Description: "pull the chart",
		Cmd:         `echo {{ .Values.missing }} > out.txt`,
	}

	err := runCreateActions(context.Background(), dir, zarf.ZarfComponentActionDefaults{}, []zarf.ZarfComponentAction{action}, value.Values{}, newVariableConfig(context.Background()))
	if err == nil {
		t.Fatal("want an error for a value the package does not define")
	}
	if !strings.Contains(err.Error(), "pull the chart") {
		t.Fatalf("error does not name the action: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "out.txt")); statErr == nil {
		t.Fatal("the action ran despite failing to render")
	}
}

// Rendering must not write back into the package: a wait block hangs off the
// action by pointer, and the same pointer is what ends up in zarf.yaml.
func TestRenderActionDoesNotMutateTheWaitBlock(t *testing.T) {
	action := zarf.ZarfComponentAction{
		Template: templated(),
		Wait: &zarf.ZarfComponentActionWait{
			Cluster: &zarf.ZarfComponentActionWaitCluster{
				Kind: "Pod",
				Name: `{{ .Values.name }}`,
			},
		},
	}

	rendered, err := renderAction(context.Background(), action, value.Values{"name": "podinfo"}, newVariableConfig(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	if rendered.Wait.Cluster.Name != "podinfo" {
		t.Fatalf("got %q, want the rendered name", rendered.Wait.Cluster.Name)
	}
	if action.Wait.Cluster.Name != `{{ .Values.name }}` {
		t.Fatalf("the original action was rewritten: %q", action.Wait.Cluster.Name)
	}
	if rendered.ShouldTemplate() {
		t.Fatal("the rendered action is still flagged for templating, so Zarf would render it again")
	}
}
