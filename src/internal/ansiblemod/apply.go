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

// applyParams is the cargoship_apply module's parameter surface.
//
// Apply is the widest action cargoship has, and the parameters follow its flags one for one. The
// optional switches are pointers for the reason given on hostUpdates: an unset parameter renders
// no flag, so whatever the cargoship configuration file set still applies.
type applyParams struct {
	common
	hostUpdates
	verification

	// Package is the distro package to install, the positional argument of the command.
	Package string `json:"package"`

	Concurrency         *int     `json:"concurrency"`
	WorkConcurrency     string   `json:"work_concurrency"`
	LabelNodes          *bool    `json:"label_nodes"`
	AllowUnmanagedNodes *bool    `json:"allow_unmanaged_nodes"`
	UpdateKubeconfig    *bool    `json:"update_kubeconfig"`
	Kubeconfig          string   `json:"kubeconfig"`
	Values              []string `json:"values"`
	Timeout             string   `json:"timeout"`

	// Key material is named by path, never by value. See engineConfigSyncParams.
	VaultPasswordFile string   `json:"vault_password_file"`
	AgeIdentityFiles  []string `json:"age_identity_file"`
}

// runApply installs or converges the cluster described by an Ansible inventory.
func runApply(ctx context.Context, exec Exec, args *Args, resp *Response) error {
	var p applyParams
	if err := args.Params(&p); err != nil {
		return err
	}
	if p.Package == "" {
		return errors.New(`the "package" parameter is required: it is the distro package to install`)
	}
	if err := p.validate(); err != nil {
		return err
	}

	return runAction(ctx, exec, resp, &p.common, args.Control,
		func(inventory string) []string {
			return buildApplyArgs(&p, inventory, args.Control)
		},
		messages{
			done:  "applied the cluster across the fleet",
			check: "check mode: reported the phases that would run against the fleet",
		})
}

// buildApplyArgs renders the parameters as the command line an operator would have typed.
func buildApplyArgs(p *applyParams, inventory string, control Control) []string {
	c := begin("apply", []string{p.Package}, inventory, control,
		surface{confirm: true, dryRun: true})

	c.intFlag(flagConcurrency, p.Concurrency)
	c.flag(flagWorkConcurrency, p.WorkConcurrency)
	c.updates(p.hostUpdates)
	c.boolFlag(flagLabelNodes, p.LabelNodes)
	c.boolFlag(flagAllowUnmanagedNodes, p.AllowUnmanagedNodes)
	c.boolFlag(flagUpdateKubeConfig, p.UpdateKubeconfig)
	c.flag(flagKubeConfig, p.Kubeconfig)
	c.flag(flagVaultPasswordFile, p.VaultPasswordFile)
	c.repeated(flagAgeIdentityFile, p.AgeIdentityFiles)
	c.repeated(flagValues, p.Values)
	c.flag(flagTimeout, p.Timeout)
	c.verify(p.verification)
	p.finish(c, control)

	return c.args()
}
