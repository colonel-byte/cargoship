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

import "context"

// resetParams is the cargoship_reset module's parameter surface.
//
// Reset takes no package: it removes what is installed, and the distro it removes is named
// directly rather than read out of one.
type resetParams struct {
	common
	hostUpdates

	// Distro is the distro to remove, k3s or rke2.
	Distro string `json:"distro"`

	Concurrency     *int   `json:"concurrency"`
	WorkConcurrency string `json:"work_concurrency"`
}

// runReset removes the cluster from the fleet described by an Ansible inventory.
func runReset(ctx context.Context, exec Exec, args *Args, resp *Response) error {
	var p resetParams
	if err := args.Params(&p); err != nil {
		return err
	}
	if err := p.validate(); err != nil {
		return err
	}

	return runAction(ctx, exec, resp, &p.common, args.Control,
		func(inventory string) []string {
			return buildResetArgs(&p, inventory, args.Control)
		},
		messages{
			done:  "reset the fleet",
			check: "check mode: reported the phases that would run against the fleet",
		})
}

// buildResetArgs renders the parameters as the command line an operator would have typed.
func buildResetArgs(p *resetParams, inventory string, control Control) []string {
	c := begin("reset", nil, inventory, control, surface{confirm: true, dryRun: true})

	c.flag(flagDistro, p.Distro)
	c.intFlag(flagConcurrency, p.Concurrency)
	c.flag(flagWorkConcurrency, p.WorkConcurrency)
	c.updates(p.hostUpdates)
	p.finish(c, control)

	return c.args()
}
