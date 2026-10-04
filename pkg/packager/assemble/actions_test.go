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

// ptr is the one-line form the pointer fields of an action need.
func ptr[T any](v T) *T { return &v }

// Zarf v0.87.0 moved actions.Run onto its normalized api types, and toAPIActionSet
// is the only place cargoship's v1alpha1 action set crosses over. A field that
// stops being carried across does not fail a build -- the action runs without it,
// so a muted command starts printing or a retry stops happening.
func TestToAPIActionSetMapsEveryField(t *testing.T) {
	set := zarf.ZarfComponentActionSet{
		Defaults: zarf.ZarfComponentActionDefaults{
			Mute:            true,
			MaxTotalSeconds: 90,
			MaxRetries:      2,
			Dir:             "/defaults",
			Env:             []string{"FROM=defaults"},
			Shell:           zarf.Shell{Linux: "bash", Darwin: "zsh", Windows: "pwsh"},
		},
		Before: []zarf.ZarfComponentAction{
			{
				Mute:            ptr(true),
				MaxTotalSeconds: ptr(30),
				MaxRetries:      ptr(3),
				Dir:             ptr("/action"),
				Env:             []string{"FROM=action"},
				Cmd:             "echo hello",
				Shell:           &zarf.Shell{Linux: "sh"},
				Description:     "say hello",
				Template:        templated(),
				SetVariables:    []zarf.Variable{{Name: "GREETING"}},
				SetValues:       []zarf.SetValue{{Key: "greeting", Value: "hello", Type: "string"}},
				Wait: &zarf.ZarfComponentActionWait{
					Cluster: &zarf.ZarfComponentActionWaitCluster{
						Kind:      "Pod",
						Name:      "podinfo",
						Namespace: "default",
						Condition: "Ready",
					},
					Network: &zarf.ZarfComponentActionWaitNetwork{
						Protocol: "http",
						Address:  "127.0.0.1:8080",
						Code:     200,
					},
				},
			},
		},
	}

	got := toAPIActionSet(set)

	defaults := got.Defaults
	if !defaults.Silent {
		t.Error("defaults.mute did not become defaults.Silent")
	}
	if defaults.MaxTotalSeconds != 90 {
		t.Errorf("defaults.MaxTotalSeconds = %d, want 90", defaults.MaxTotalSeconds)
	}
	if defaults.Retries != 2 {
		t.Errorf("defaults.maxRetries did not become defaults.Retries: %d", defaults.Retries)
	}
	if defaults.Dir != "/defaults" {
		t.Errorf("defaults.Dir = %q", defaults.Dir)
	}
	if len(defaults.Env) != 1 || defaults.Env[0] != "FROM=defaults" {
		t.Errorf("defaults.Env = %v", defaults.Env)
	}
	if defaults.Shell.Linux != "bash" || defaults.Shell.Darwin != "zsh" || defaults.Shell.Windows != "pwsh" {
		t.Errorf("defaults.Shell = %+v", defaults.Shell)
	}

	if len(got.Before) != 1 {
		t.Fatalf("got %d actions, want 1", len(got.Before))
	}
	action := got.Before[0]
	if action.Silent == nil || !*action.Silent {
		t.Error("mute did not become Silent")
	}
	if action.MaxTotalSeconds == nil || *action.MaxTotalSeconds != 30 {
		t.Errorf("MaxTotalSeconds = %v", action.MaxTotalSeconds)
	}
	if action.Retries == nil || *action.Retries != 3 {
		t.Errorf("maxRetries did not become Retries: %v", action.Retries)
	}
	if action.Dir == nil || *action.Dir != "/action" {
		t.Errorf("Dir = %v", action.Dir)
	}
	if len(action.Env) != 1 || action.Env[0] != "FROM=action" {
		t.Errorf("Env = %v", action.Env)
	}
	if action.Cmd != "echo hello" {
		t.Errorf("Cmd = %q", action.Cmd)
	}
	if action.Shell == nil || action.Shell.Linux != "sh" {
		t.Errorf("Shell = %+v", action.Shell)
	}
	if action.Description != "say hello" {
		t.Errorf("Description = %q", action.Description)
	}
	if !action.EnableTemplating {
		t.Error("template did not become EnableTemplating")
	}
	if len(action.SetVariables) != 1 || action.SetVariables[0].Name != "GREETING" {
		t.Errorf("SetVariables = %+v", action.SetVariables)
	}
	if len(action.SetValues) != 1 || action.SetValues[0].Key != "greeting" || action.SetValues[0].Type != "string" {
		t.Errorf("SetValues = %+v", action.SetValues)
	}
	if action.Wait == nil || action.Wait.Cluster == nil || action.Wait.Network == nil {
		t.Fatalf("Wait = %+v", action.Wait)
	}
	cluster := action.Wait.Cluster
	if cluster.Kind != "Pod" || cluster.Name != "podinfo" || cluster.Namespace != "default" {
		t.Errorf("Wait.Cluster = %+v", cluster)
	}
	// The condition went from a string to a struct, and only the expression half is
	// what zarf waits on.
	if cluster.Condition.Expression != "Ready" {
		t.Errorf("Wait.Cluster.Condition.Expression = %q", cluster.Condition.Expression)
	}
	network := action.Wait.Network
	if network.Protocol != "http" || network.Address != "127.0.0.1:8080" || network.Code != 200 {
		t.Errorf("Wait.Network = %+v", network)
	}
}

// Rendering clears the template flag so that Zarf runs what cargoship already
// rendered rather than rendering it twice. That only holds while a cleared flag
// converts to EnableTemplating being false.
func TestToAPIActionSetCarriesAClearedTemplateFlag(t *testing.T) {
	rendered, err := renderAction(context.Background(), zarf.ZarfComponentAction{
		Template: templated(),
		Cmd:      `echo {{ .Values.greeting }}`,
	}, value.Values{"greeting": "hello"}, newVariableConfig(context.Background()))
	if err != nil {
		t.Fatal(err)
	}

	got := toAPIActionSet(zarf.ZarfComponentActionSet{
		Before: []zarf.ZarfComponentAction{rendered},
	})
	if got.Before[0].EnableTemplating {
		t.Fatal("the rendered action is still flagged for templating, so Zarf would render it again")
	}
	if got.Before[0].Cmd != "echo hello" {
		t.Fatalf("Cmd = %q, want the already-rendered command", got.Before[0].Cmd)
	}
}

// Conversion runs Zarf's own package migrations, which is a behavior change worth
// pinning: the deprecated singular setVariable was read by nothing on this path
// before, because zarf's runAction only ever looked at SetVariables.
func TestToAPIActionSetMigratesTheDeprecatedSetVariable(t *testing.T) {
	t.Run("the singular form becomes a list entry", func(t *testing.T) {
		got := toAPIActionSet(zarf.ZarfComponentActionSet{
			Before: []zarf.ZarfComponentAction{
				{
					Cmd:                   "echo hello",
					DeprecatedSetVariable: "GREETING",
				},
			},
		})
		if len(got.Before[0].SetVariables) != 1 || got.Before[0].SetVariables[0].Name != "GREETING" {
			t.Fatalf("SetVariables = %+v, want the migrated singular form", got.Before[0].SetVariables)
		}
	})

	t.Run("an explicit list wins over the singular form", func(t *testing.T) {
		got := toAPIActionSet(zarf.ZarfComponentActionSet{
			Before: []zarf.ZarfComponentAction{
				{
					Cmd:                   "echo hello",
					DeprecatedSetVariable: "OLD",
					SetVariables:          []zarf.Variable{{Name: "NEW"}},
				},
			},
		})
		if len(got.Before[0].SetVariables) != 1 || got.Before[0].SetVariables[0].Name != "NEW" {
			t.Fatalf("SetVariables = %+v, want only the explicit list", got.Before[0].SetVariables)
		}
	})
}

// Zarf's migration rewrites the action slice it is handed in place, so the set
// handed to it is built per action rather than being the caller's own. A package
// whose actions were rewritten here would be written back out to distro.yaml
// carrying migrations the author never made.
func TestRunCreateActionsDoesNotRewriteItsInput(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	list := []zarf.ZarfComponentAction{
		{
			Cmd:                   "echo from-the-deprecated-field > out.txt",
			DeprecatedSetVariable: "GREETING",
		},
	}

	if err := runCreateActions(ctx, dir, zarf.ZarfComponentActionDefaults{}, list, nil, newVariableConfig(ctx)); err != nil {
		t.Fatal(err)
	}
	if list[0].DeprecatedSetVariable != "GREETING" {
		t.Errorf("the deprecated field was cleared on the caller's action: %q", list[0].DeprecatedSetVariable)
	}
	if len(list[0].SetVariables) != 0 {
		t.Errorf("the migration wrote back into the caller's action: %+v", list[0].SetVariables)
	}
}

// The end of that behavior change: a variable named by the deprecated singular
// field now reaches the action after it, the way the list form always has.
func TestRunCreateActionsHonorsTheDeprecatedSetVariable(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	list := []zarf.ZarfComponentAction{
		{
			Cmd:                   `echo from-the-deprecated-field`,
			DeprecatedSetVariable: "GREETING",
		},
		{
			Template: templated(),
			Cmd:      `echo {{ .Variables.GREETING }} > out.txt`,
		},
	}

	if err := runCreateActions(ctx, dir, zarf.ZarfComponentActionDefaults{}, list, nil, newVariableConfig(ctx)); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != "from-the-deprecated-field" {
		t.Fatalf("got %q, want the variable the deprecated field set", got)
	}
}
