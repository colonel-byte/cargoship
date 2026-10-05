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

// kubeConfigParams is the cargoship_kube_config module's parameter surface.
//
// This is the one action that touches the management node rather than the fleet: it fetches the
// cluster's kubeconfig from a controller and writes it locally.
type kubeConfigParams struct {
	common

	// Distro is the distro the cluster runs, k3s or rke2.
	Distro string `json:"distro"`
	// Kubeconfig is where to write the file.
	Kubeconfig string `json:"kubeconfig"`
}

// runKubeConfig fetches the cluster kubeconfig onto the management node.
func runKubeConfig(ctx context.Context, exec Exec, args *Args, resp *Response) error {
	var p kubeConfigParams
	if err := args.Params(&p); err != nil {
		return err
	}
	if err := p.validate(); err != nil {
		return err
	}

	return runAction(ctx, exec, resp, &p.common, args.Control,
		func(inventory string) []string {
			return buildKubeConfigArgs(&p, inventory, args.Control)
		},
		messages{
			done: "wrote the cluster kubeconfig",
			// `cargoship kube-config` has no dry run, so there is nothing to report and
			// nothing safe to do. See runAction.
			noCheck: "check mode: cargoship kube-config has no dry run, so the task was skipped rather than run",
		})
}

// buildKubeConfigArgs renders the parameters as the command line an operator would have typed.
//
// There is no --confirm and no --dry-run: the command has neither.
func buildKubeConfigArgs(p *kubeConfigParams, inventory string, control Control) []string {
	c := begin("kube-config", nil, inventory, control, surface{})

	c.flag(flagDistro, p.Distro)
	c.flag(flagKubeConfig, p.Kubeconfig)
	p.finish(c, control)

	return c.args()
}
