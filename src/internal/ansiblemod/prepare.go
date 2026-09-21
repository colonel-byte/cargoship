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

package ansiblemod

import (
	"context"
	"errors"
)

// prepareParams is the cargoship_prepare module's parameter surface.
//
// Prepare stages a package onto the fleet and readies the hosts. It takes no key material: it
// neither reads an encrypted value nor writes a kubeconfig, so there is no vault password and no
// age identity to name.
type prepareParams struct {
	common
	hostUpdates
	verification

	// Package is the distro package to stage, the positional argument of the command.
	Package string `json:"package"`

	Concurrency     *int     `json:"concurrency"`
	WorkConcurrency string   `json:"work_concurrency"`
	Values          []string `json:"values"`
	Timeout         string   `json:"timeout"`
}

// runPrepare stages a package across the fleet described by an Ansible inventory.
func runPrepare(ctx context.Context, exec Exec, args *Args, resp *Response) error {
	var p prepareParams
	if err := args.Params(&p); err != nil {
		return err
	}
	if p.Package == "" {
		return errors.New(`the "package" parameter is required: it is the distro package to stage`)
	}
	if err := p.validate(); err != nil {
		return err
	}

	return runAction(ctx, exec, resp, &p.common, args.Control,
		func(inventory string) []string {
			return buildPrepareArgs(&p, inventory, args.Control)
		},
		messages{
			done:  "prepared the fleet",
			check: "check mode: reported the phases that would run against the fleet",
		})
}

// buildPrepareArgs renders the parameters as the command line an operator would have typed.
func buildPrepareArgs(p *prepareParams, inventory string, control Control) []string {
	c := begin("prepare", []string{p.Package}, inventory, control,
		surface{confirm: true, dryRun: true})

	c.intFlag(flagConcurrency, p.Concurrency)
	c.flag(flagWorkConcurrency, p.WorkConcurrency)
	c.updates(p.hostUpdates)
	c.repeated(flagValues, p.Values)
	c.flag(flagTimeout, p.Timeout)
	c.verify(p.verification)
	p.finish(c, control)

	return c.args()
}
