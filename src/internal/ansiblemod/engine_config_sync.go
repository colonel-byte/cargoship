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
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/colonel-byte/cargoship/src/internal/ansibleinv"
	goyaml "github.com/goccy/go-yaml"
)

// The flags of `cargoship engine-config-sync` this module renders. They are spelled out rather
// than referenced from src/cmd for the reason given on Exec, and TestEngineConfigSyncArgsParse in
// src/cmd parses a fully populated argument vector against the real command so that a rename in
// the CLI fails a test rather than a playbook.
const (
	flagConfig            = "config"
	flagConfirm           = "confirm"
	flagDryRun            = "dry-run"
	flagConcurrency       = "concurrency"
	flagWorkConcurrency   = "work-concurrency"
	flagLabelNodes        = "label-nodes"
	flagUpdateKubeConfig  = "update-kubeconfig"
	flagKubeConfig        = "kubeconfig"
	flagVaultPasswordFile = "vault-password-file"
	flagAgeIdentityFile   = "age-identity-file"
	flagValues            = "values"
	flagTimeout           = "timeout"
	flagLogLevel          = "log-level"
	flagLogFormat         = "log-format"
	flagNoColor           = "no-color"
)

// engineConfigSyncParams is the cargoship_engine_config_sync module's parameter surface.
//
// The names are snake_case because that is what an operator writes in a playbook, while the
// inventory argument keeps the shape `cargoship inventory from-ansible` already reads, so one
// projection of an inventory can be fed to either without being rewritten.
//
// The optional booleans and the integer are pointers so that "the operator said nothing" is a
// different state from "the operator said false". Several of these flags default from the
// cargoship configuration file, and a module that always passed a value would quietly overrule a
// default a team had committed.
type engineConfigSyncParams struct {
	// Package is the distro package to synchronise from, the positional argument of the
	// command.
	Package string `json:"package"`
	// Inventory is the resolved Ansible inventory, as the action plugin projects it.
	Inventory *ansibleinv.Request `json:"inventory"`
	// InventoryPath is where to write the generated ZarfCluster document. When it is empty the
	// module writes a private temporary file and removes it after a run that succeeded.
	InventoryPath string `json:"inventory_path"`

	Concurrency      *int     `json:"concurrency"`
	WorkConcurrency  string   `json:"work_concurrency"`
	LabelNodes       *bool    `json:"label_nodes"`
	UpdateKubeconfig *bool    `json:"update_kubeconfig"`
	Kubeconfig       string   `json:"kubeconfig"`
	Values           []string `json:"values"`
	Timeout          string   `json:"timeout"`
	LogLevel         string   `json:"log_level"`
	LogFormat        string   `json:"log_format"`

	// Key material is named by path, never by value. A module's parameters are written into the
	// arguments file Ansible leaves on disk, so a password passed by value would be a password
	// written to a file nobody chose the mode of.
	VaultPasswordFile string   `json:"vault_password_file"`
	AgeIdentityFiles  []string `json:"age_identity_file"`
}

// runEngineConfigSync converges engine configuration across the fleet described by an Ansible
// inventory.
func runEngineConfigSync(ctx context.Context, exec Exec, args *Args, resp *Response) error {
	var p engineConfigSyncParams
	if err := args.Params(&p); err != nil {
		return err
	}
	if p.Package == "" {
		return errors.New(`the "package" parameter is required: it is the distro package to synchronise from`)
	}
	if p.Inventory == nil {
		return errors.New(`the "inventory" parameter is required: it is the resolved Ansible inventory the cluster is derived from`)
	}

	path, tempDir, err := writeInventory(&p)
	if err != nil {
		return err
	}
	resp.Cargoship.InventoryPath = path
	resp.Cargoship.InventoryKept = true

	argv := buildEngineConfigSyncArgs(&p, path, args.Control)
	resp.Cargoship.Command = append([]string{"cargoship"}, argv...)

	if runErr := exec(ctx, argv); runErr != nil {
		// The generated document stays on disk. It is the first thing to look at when a run
		// fails for a reason that reads like the wrong cluster, and an operator who has to
		// reproduce the translation to see it has lost the run that produced it.
		return runErr
	}

	if tempDir != "" {
		if err := os.RemoveAll(tempDir); err != nil {
			return fmt.Errorf("the run succeeded but the generated inventory could not be removed: %w", err)
		}
		resp.Cargoship.InventoryKept = false
	}

	// Phases do not yet report whether they changed anything, so this is a convention rather
	// than an observation, and ChangedSignal says so. True is the safe direction: an Ansible
	// handler that fires when nothing happened is a smaller failure than one that stays silent
	// when something did.
	resp.Changed = true
	if args.Control.CheckMode {
		resp.Msg = "check mode: reported the phases that would run against the fleet"
		return nil
	}
	resp.Msg = "synchronised engine configuration across the fleet"
	return nil
}

// writeInventory translates the Ansible inventory and writes the document cargoship installs from.
//
// It returns the path written, and the directory to remove once the run has succeeded, which is
// empty when the operator named the path themselves.
func writeInventory(p *engineConfigSyncParams) (string, string, error) {
	out, err := ansibleinv.Translate(p.Inventory.Input, p.Inventory.Cluster)
	if err != nil {
		return "", "", err
	}
	doc, err := goyaml.Marshal(out)
	if err != nil {
		return "", "", fmt.Errorf("unable to serialize the generated inventory: %w", err)
	}

	path := p.InventoryPath
	tempDir := ""
	if path == "" {
		// A directory rather than a bare temporary file, so the mode is set before the name
		// exists rather than after: a predictable name under a world-writable directory is a
		// symlink waiting to be planted.
		tempDir, err = os.MkdirTemp("", "cargoship-ansible-")
		if err != nil {
			return "", "", fmt.Errorf("unable to create a directory for the generated inventory: %w", err)
		}
		path = filepath.Join(tempDir, "inventory.yaml")
	}

	// The document carries connection details and may carry credentials, so it is written with
	// the same mode the rest of cargoship writes an inventory with.
	if err := os.WriteFile(path, doc, 0o600); err != nil {
		return "", "", fmt.Errorf("unable to write the generated inventory to %s: %w", path, err)
	}
	return path, tempDir, nil
}

// buildEngineConfigSyncArgs renders the parameters as the command line an operator would have
// typed.
func buildEngineConfigSyncArgs(p *engineConfigSyncParams, inventory string, control Control) []string {
	argv := []string{
		"engine-config-sync", p.Package,
		"--" + flagConfig, inventory,
		// A module that asked for confirmation would never get it: there is no terminal on the
		// other end. The playbook task is the confirmation.
		"--" + flagConfirm,
		// Ansible captures stderr as a text blob, and escape sequences in it are noise in
		// every report that blob ends up in.
		"--" + flagNoColor,
	}

	// Check mode is the existing dry run. Phases opt into it one at a time -- a phase that has
	// said nothing about how it behaves is reported and not run -- so nothing has to be trusted
	// here beyond the flag.
	if control.CheckMode {
		argv = append(argv, "--"+flagDryRun)
	}

	if p.Concurrency != nil {
		argv = append(argv, "--"+flagConcurrency, strconv.Itoa(*p.Concurrency))
	}
	if p.WorkConcurrency != "" {
		argv = append(argv, "--"+flagWorkConcurrency, p.WorkConcurrency)
	}
	if p.LabelNodes != nil {
		argv = append(argv, "--"+flagLabelNodes+"="+strconv.FormatBool(*p.LabelNodes))
	}
	if p.UpdateKubeconfig != nil {
		argv = append(argv, "--"+flagUpdateKubeConfig+"="+strconv.FormatBool(*p.UpdateKubeconfig))
	}
	if p.Kubeconfig != "" {
		argv = append(argv, "--"+flagKubeConfig, p.Kubeconfig)
	}
	if p.VaultPasswordFile != "" {
		argv = append(argv, "--"+flagVaultPasswordFile, p.VaultPasswordFile)
	}
	for _, file := range p.AgeIdentityFiles {
		argv = append(argv, "--"+flagAgeIdentityFile, file)
	}
	for _, file := range p.Values {
		argv = append(argv, "--"+flagValues, file)
	}
	if p.Timeout != "" {
		argv = append(argv, "--"+flagTimeout, p.Timeout)
	}
	if level := logLevel(p, control); level != "" {
		argv = append(argv, "--"+flagLogLevel, level)
	}
	if p.LogFormat != "" {
		argv = append(argv, "--"+flagLogFormat, p.LogFormat)
	}

	return argv
}

// logLevel resolves the log level, letting the parameter win over Ansible's verbosity.
//
// Mapping verbosity at all is worth it because the alternative is asking an operator to edit the
// playbook to find out why a task failed, when they have already typed -v to ask exactly that.
func logLevel(p *engineConfigSyncParams, control Control) string {
	if p.LogLevel != "" {
		return p.LogLevel
	}
	if control.Verbosity > 0 {
		return "debug"
	}
	return ""
}
