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

	"github.com/colonel-byte/cargoship/internal/ansibleinv"
	"github.com/colonel-byte/cargoship/internal/clustercfg"
	"github.com/colonel-byte/cargoship/pkg/phase"
	goyaml "github.com/goccy/go-yaml"
)

// The flags of the cargoship install commands the modules render. They are spelled out rather than
// referenced from src/cmd for the reason given on Exec, and TestModuleArgsParse in src/cmd parses
// each module's widest argument vector against the real command, so that a rename in the CLI fails
// a test rather than a playbook.
const (
	flagConfig              = "config"
	flagConfirm             = "confirm"
	flagDryRun              = "dry-run"
	flagDistro              = "distro"
	flagConcurrency         = "concurrency"
	flagWorkConcurrency     = "work-concurrency"
	flagHosts               = "hosts"
	flagFirewall            = "firewall"
	flagFAPolicyd           = "fapolicyd"
	flagLabelNodes          = "label-nodes"
	flagAllowUnmanagedNodes = "allow-unmanaged-nodes"
	flagUpdateKubeConfig    = "update-kubeconfig"
	flagKubeConfig          = "kubeconfig"
	flagVaultPasswordFile   = "vault-password-file"
	flagAgeIdentityFile     = "age-identity-file"
	flagValues              = "values"
	flagTimeout             = "timeout"
	flagKey                 = "key"
	flagVerify              = "verify"
	flagLogLevel            = "log-level"
	flagLogFormat           = "log-format"
	flagLogFile             = "log-file"
	flagNoColor             = "no-color"
)

// common is the parameter surface every module carries.
//
// It is embedded rather than repeated so that one module cannot spell inventory_path differently
// from its neighbour. Args.Params still names an unknown parameter across it, because
// jsonFieldNames follows embedding the way encoding/json does.
type common struct {
	// Inventory is the resolved Ansible inventory the action plugin projects into the task.
	Inventory *ansibleinv.Request `json:"inventory"`
	// InventoryPath is where to write the translated document. A private temporary file when
	// the operator did not choose.
	InventoryPath string `json:"inventory_path"`

	LogLevel  string `json:"log_level"`
	LogFormat string `json:"log_format"`
	LogFile   *bool  `json:"log_file"`

	// AgeRecipients are optional public keys used to encrypt registry credentials in the generated inventory document on disk.
	AgeRecipients     []string `json:"age_recipient"`
	AgeRecipientFiles []string `json:"age_recipients_file"`
}

// hostUpdates are the three host preparation switches apply, prepare and reset share.
//
// They are pointers for the reason all the optional flags are: an unset parameter has to render
// no flag at all, or the module overrules whatever the cargoship configuration file set, which is
// the opposite of an operator saying nothing.
type hostUpdates struct {
	Hosts     *bool `json:"hosts"`
	Firewall  *bool `json:"firewall"`
	FAPolicyd *bool `json:"fapolicyd"`
}

// verification is the package signature surface the modules expose.
//
// Only the key-based flags are here. Keyless verification identifies a signer against a Fulcio
// root and a transparency log, which wants network the management node this runs on does not
// have; and its flags are mutually exclusive with each other in ways cobra enforces and a
// hand-written module would have to restate. An operator who needs keyless verification runs
// `cargoship package verify` before the play.
type verification struct {
	PublicKey string `json:"public_key"`
	Verify    string `json:"verify"`
}

// surface is what an action's command line supports beyond its own flags.
type surface struct {
	// confirm is whether the action refuses to run without --confirm.
	confirm bool
	// dryRun is whether the action has a --dry-run for check mode to use. An action without one
	// reports the task as skipped rather than running it, which is what Ansible does with a
	// module that has declared it does not support check mode.
	dryRun bool
}

// command accumulates the argument vector a module renders. Every flag it adds is optional in the
// same way: a parameter the operator did not set adds nothing.
type command struct {
	argv []string
}

// newCommand starts a command line with the action and its positional arguments.
func newCommand(action string, positional ...string) *command {
	c := &command{argv: []string{action}}
	c.argv = append(c.argv, positional...)
	return c
}

// bare adds a flag that takes no value.
func (c *command) bare(name string) {
	c.argv = append(c.argv, "--"+name)
}

// flag adds a flag and its value, and adds nothing when the value is empty.
func (c *command) flag(name, value string) {
	if value == "" {
		return
	}
	c.argv = append(c.argv, "--"+name, value)
}

// boolFlag adds a flag written as --name=value, which is how a boolean flag is given a false
// value on a cobra command line. Nothing is added when the operator said nothing.
func (c *command) boolFlag(name string, value *bool) {
	if value == nil {
		return
	}
	c.argv = append(c.argv, "--"+name+"="+strconv.FormatBool(*value))
}

// intFlag adds a flag and its integer value, and adds nothing when the operator said nothing.
func (c *command) intFlag(name string, value *int) {
	if value == nil {
		return
	}
	c.argv = append(c.argv, "--"+name, strconv.Itoa(*value))
}

// repeated adds one copy of the flag per value, which is how a string array flag is given more
// than one.
func (c *command) repeated(name string, values []string) {
	for _, value := range values {
		c.argv = append(c.argv, "--"+name, value)
	}
}

// updates adds the three host preparation switches.
func (c *command) updates(u hostUpdates) {
	c.boolFlag(flagHosts, u.Hosts)
	c.boolFlag(flagFirewall, u.Firewall)
	c.boolFlag(flagFAPolicyd, u.FAPolicyd)
}

// verify adds the package signature flags.
func (c *command) verify(v verification) {
	c.flag(flagKey, v.PublicKey)
	c.flag(flagVerify, v.Verify)
}

// args is the finished argument vector.
func (c *command) args() []string {
	return c.argv
}

// begin starts the command line every module renders: the action, its positional arguments, the
// generated inventory, and the two things a module always says for itself.
func begin(action string, positional []string, inventory string, control Control, s surface) *command {
	c := newCommand(action, positional...)
	c.flag(flagConfig, inventory)

	if s.confirm {
		// A module that asked for confirmation would never get it: there is no terminal on the
		// other end. The playbook task is the confirmation.
		c.bare(flagConfirm)
	}

	// Ansible captures stderr as a text blob, and escape sequences in it are noise in every
	// report that blob ends up in.
	c.bare(flagNoColor)

	// Check mode is the existing dry run. Phases opt into it one at a time -- a phase that has
	// said nothing about how it behaves is reported and not run -- so nothing has to be trusted
	// here beyond the flag.
	if control.CheckMode && s.dryRun {
		c.bare(flagDryRun)
	}

	return c
}

// finish adds the flags every module ends with.
func (p *common) finish(c *command, control Control) {
	c.flag(flagLogLevel, logLevel(p.LogLevel, control))
	c.flag(flagLogFormat, p.LogFormat)
	c.boolFlag(flagLogFile, p.LogFile)
}

// validate reports the parameters every module requires.
func (p *common) validate() error {
	if p.Inventory == nil {
		return errors.New(`the "inventory" parameter is required: it is the resolved Ansible inventory the cluster is derived from`)
	}
	return nil
}

// logLevel resolves the log level, letting the parameter win over Ansible's verbosity.
//
// Mapping verbosity at all is worth it because the alternative is asking an operator to edit the
// playbook to find out why a task failed, when they have already typed -v to ask exactly that.
func logLevel(param string, control Control) string {
	if param != "" {
		return param
	}
	if control.Verbosity > 0 {
		return "debug"
	}
	return ""
}

// writeInventory translates the Ansible inventory and writes the document cargoship installs from.
//
// It returns the path written, and the directory to remove once the run has succeeded, which is
// empty when the operator named the path themselves.
func writeInventory(p *common) (string, string, error) {
	out, err := ansibleinv.Translate(p.Inventory.Input, p.Inventory.Cluster)
	if err != nil {
		return "", "", err
	}
	doc, err := goyaml.Marshal(out)
	if err != nil {
		return "", "", fmt.Errorf("unable to serialize the generated inventory: %w", err)
	}

	if len(p.AgeRecipients) > 0 || len(p.AgeRecipientFiles) > 0 {
		keyring, err := clustercfg.ResolveKeyring(clustercfg.KeyOptions{
			AgeRecipients:     p.AgeRecipients,
			AgeRecipientFiles: p.AgeRecipientFiles,
		})
		if err != nil {
			return "", "", fmt.Errorf("resolving encryption recipients: %w", err)
		}
		if !keyring.Empty() {
			encrypted, _, _, err := clustercfg.EncryptConfig(doc, keyring, false)
			if err != nil {
				return "", "", fmt.Errorf("encrypting inventory credentials: %w", err)
			}
			doc = encrypted
		}
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

// messages are what a module says about a run that worked.
type messages struct {
	// done is reported after a real run, check after a check-mode one.
	done  string
	check string
	// noCheck is reported instead of running anything when the operator asked for check mode
	// and the action has no dry run. It is empty for an action that has one.
	noCheck string
}

// runAction is the body every module shares: write the generated inventory, render the command
// line, run it, and report what the phases did.
//
// build is given the path the inventory was written to, because the path is what the command line
// is built around and it is not known until the document exists.
func runAction(
	ctx context.Context,
	exec Exec,
	resp *Response,
	p *common,
	control Control,
	build func(inventory string) []string,
	msg messages,
) error {
	if control.CheckMode && msg.noCheck != "" {
		// Ansible skips a task whose module has declared it cannot check. A binary module has
		// nowhere to declare that, so it says so in its result instead. Running anyway would
		// change a fleet during a run an operator asked to be told about.
		resp.Skipped = true
		resp.Msg = msg.noCheck
		resp.Cargoship.ChangedSignal = SignalUnknown
		return nil
	}

	path, tempDir, err := writeInventory(p)
	if err != nil {
		return err
	}
	resp.Cargoship.InventoryPath = path
	resp.Cargoship.InventoryKept = true

	argv := build(path)
	resp.Cargoship.Command = append([]string{"cargoship"}, argv...)

	// The command's only return value is an error, so what the phases did comes back on the
	// context. See phase.ResultSink.
	ctx, sink := phase.WithResultSink(ctx)

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

	reportChanged(resp, sink, control.CheckMode)
	if control.CheckMode {
		resp.Msg = msg.check
		return nil
	}
	resp.Msg = msg.done
	return nil
}
