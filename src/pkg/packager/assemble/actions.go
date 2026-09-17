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
	"fmt"

	"github.com/colonel-byte/cargoship/src/pkg/helmvalues"
	zarf "github.com/zarf-dev/zarf/src/api/v1alpha1"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/packager/actions"
	"github.com/zarf-dev/zarf/src/pkg/template"
	"github.com/zarf-dev/zarf/src/pkg/value"
	"github.com/zarf-dev/zarf/src/pkg/variables"
)

// newVariableConfig builds the variable configuration the onCreate actions share.
//
// Zarf builds one of these itself when it is handed a nil config, but it builds a
// fresh one per call, so a variable an action sets is gone by the next call.
// Building it here keeps setVariables working across the whole onCreate set, and
// gives runCreateActions something to read those variables back out of when it
// renders the action that references them.
//
// Nothing prompts: a package build is not interactive, so an interactive variable
// resolves to its default.
func newVariableConfig(ctx context.Context) *variables.VariableConfig {
	prompt := func(variable zarf.InteractiveVariable) (string, error) {
		return variable.Default, nil
	}
	return variables.New("zarf", prompt, logger.From(ctx))
}

// runCreateActions runs a set of onCreate actions, rendering each one through
// cargoship's template engine first.
//
// Zarf renders actions itself, with its own function map, and that map cannot be
// added to from outside the package: template.Apply takes no options and
// runAction builds its template objects internally. Rendering here instead is
// what makes fileExists, cachedFileExists, cachedFilePath and downloadToCache
// reachable from an action, which is what they were written for -- an action that
// pulls a chart or a tarball into the cache is the ordinary case at create time.
// helmvalues.FuncMap otherwise carries everything Zarf's does, so a template
// written against Zarf's actions renders the same way here.
//
// Each action is rendered immediately before it runs, not all of them up front,
// because an action's setValues and setVariables feed the action after it.
//
// Zarf is still what executes the action: it owns shells, retries, timeouts,
// output capture and the setValues and setVariables handling. Only the rendering
// moves.
func runCreateActions(ctx context.Context, basePath string, defaults zarf.ZarfComponentActionDefaults, list []zarf.ZarfComponentAction, values value.Values, varCfg *variables.VariableConfig) error {
	for _, action := range list {
		if action.ShouldTemplate() {
			rendered, err := renderAction(ctx, action, values, varCfg)
			if err != nil {
				return err
			}
			action = rendered
		}
		if err := actions.Run(ctx, basePath, defaults, []zarf.ZarfComponentAction{action}, varCfg, values, template.StateAccess{}); err != nil {
			return err
		}
	}
	return nil
}

// renderAction returns a copy of action with its templated fields already
// rendered and its template flag cleared, so that Zarf runs the result verbatim
// rather than rendering the rendering.
//
// The fields covered are the ones Zarf itself templates: the command, and the
// string fields of a wait action. Everything else in an action -- the shell, the
// directory, the environment -- goes through the older ###ZARF_VAR_x###
// substitution, which Zarf still applies on its own.
func renderAction(ctx context.Context, action zarf.ZarfComponentAction, values value.Values, varCfg *variables.VariableConfig) (zarf.ZarfComponentAction, error) {
	data := template.NewObjects(values).
		WithConstants(varCfg.GetConstants()).
		WithVariables(varCfg.GetSetVariableMap())

	render := func(field, s string) (string, error) {
		out, err := helmvalues.RenderTemplate(s, data, helmvalues.WithCreateFuncs(ctx))
		if err != nil {
			return "", fmt.Errorf("unable to render %s of the %q action: %w", field, actionName(action), err)
		}
		return out, nil
	}

	var err error
	if action.Cmd, err = render("cmd", action.Cmd); err != nil {
		return action, err
	}

	// The wait block hangs off the action by pointer, so it is copied before any
	// of it is rewritten: the action itself is a copy, but the block it points at
	// is the one the loaded package holds and writes back out into zarf.yaml.
	if action.Wait != nil {
		wait := *action.Wait
		if c := wait.Cluster; c != nil {
			cluster := *c
			for field, target := range map[string]*string{
				"wait.cluster.kind":      &cluster.Kind,
				"wait.cluster.name":      &cluster.Name,
				"wait.cluster.namespace": &cluster.Namespace,
				"wait.cluster.condition": &cluster.Condition,
			} {
				if *target, err = render(field, *target); err != nil {
					return action, err
				}
			}
			wait.Cluster = &cluster
		}
		if n := wait.Network; n != nil {
			network := *n
			for field, target := range map[string]*string{
				"wait.network.protocol": &network.Protocol,
				"wait.network.address":  &network.Address,
			} {
				if *target, err = render(field, *target); err != nil {
					return action, err
				}
			}
			wait.Network = &network
		}
		action.Wait = &wait
	}

	action.Template = nil
	return action, nil
}

// actionName is what an action is called in an error, preferring the description
// the author wrote over the command itself.
func actionName(action zarf.ZarfComponentAction) string {
	if action.Description != "" {
		return action.Description
	}
	return action.Cmd
}
