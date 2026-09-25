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

package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/colonel-byte/cargoship/internal/ansibleinv"
	"github.com/colonel-byte/cargoship/internal/clustercfg"
	"github.com/colonel-byte/cargoship/src/config/lang"
	goyaml "github.com/goccy/go-yaml"
	"github.com/spf13/cobra"
)

const (
	// CmdInventoryOutput flag
	CmdInventoryOutput = "output"
	// CmdInventoryName flag
	CmdInventoryName = "name"
	// CmdInventoryLoadBalancer flag
	CmdInventoryLoadBalancer = "loadbalancer"
)

type inventoryFromAnsibleOptions struct {
	output       string
	name         string
	loadBalancer string
	keyring      *keyFlags
}

// newInventoryFromAnsibleCommand turns an Ansible inventory into the cluster inventory cargoship
// installs from.
//
// The Ansible collection runs this same translation in-process, so this command installs nothing
// and exists for the two things a module cannot do: reproduce a translation outside a playbook
// run, where it can be read and diffed, and let the result be checked with `cargoship validate`
// before any of it is applied to a fleet.
//
// It does not read an inventory file. Ansible resolves the inventory -- group membership,
// group_vars, host_vars, dynamic inventory plugins, and the precedence rules over all of them --
// and this reads the resolved result.
func newInventoryFromAnsibleCommand(f *keyFlags) *cobra.Command {
	o := inventoryFromAnsibleOptions{keyring: f}

	cmd := &cobra.Command{
		Use:     "from-ansible [FILE]",
		Args:    cobra.MaximumNArgs(1),
		Short:   lang.CmdInventoryFromAnsibleShort,
		Long:    lang.CmdInventoryFromAnsibleLong,
		Example: lang.CmdInventoryFromAnsibleExample,
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.run(cmd, args)
		},
	}

	cmd.Flags().StringVarP(&o.output, CmdInventoryOutput, "o", "", lang.CmdInventoryFlagOutput)
	cmd.Flags().StringVar(&o.name, CmdInventoryName, "", lang.CmdInventoryFlagName)
	cmd.Flags().StringVar(&o.loadBalancer, CmdInventoryLoadBalancer, "", lang.CmdInventoryFlagLoadBalancer)

	return cmd
}

func (o *inventoryFromAnsibleOptions) run(cmd *cobra.Command, args []string) error {
	b, err := o.read(cmd, args)
	if err != nil {
		return err
	}

	req, err := ansibleinv.Decode(b)
	if err != nil {
		return err
	}

	// The flags win over the document, so that one projection of an inventory can be translated
	// for more than one cluster without being rewritten.
	if o.name != "" {
		req.Cluster.Name = o.name
	}
	if o.loadBalancer != "" {
		req.Cluster.LoadBalancer = o.loadBalancer
	}

	out, err := ansibleinv.Translate(req.Input, req.Cluster)
	if err != nil {
		return err
	}

	doc, err := goyaml.Marshal(out)
	if err != nil {
		return fmt.Errorf("unable to serialize the generated inventory: %w", err)
	}

	if o.keyring != nil {
		k, err := o.keyring.resolveKeyring(cmd)
		if err != nil {
			return err
		}
		if !k.Empty() {
			encrypted, _, _, err := clustercfg.EncryptConfig(doc, k, false)
			if err != nil {
				return fmt.Errorf("encrypting inventory: %w", err)
			}
			doc = encrypted
		}
	}

	return o.emit(cmd, doc)
}

// read takes the request from the named file, or from stdin when the argument is absent or "-",
// so the command composes with whatever produced the projection.
func (o *inventoryFromAnsibleOptions) read(cmd *cobra.Command, args []string) ([]byte, error) {
	if len(args) == 0 || args[0] == "-" {
		b, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil, fmt.Errorf("unable to read the Ansible inventory from stdin: %w", err)
		}
		return b, nil
	}
	b, err := os.ReadFile(args[0])
	if err != nil {
		return nil, fmt.Errorf("unable to read %s: %w", args[0], err)
	}
	return b, nil
}

// emit writes the inventory where the operator asked for it. Anything that is not the inventory
// goes to stderr, so that stdout can be redirected into a file or piped into `cargoship validate`
// unchanged.
func (o *inventoryFromAnsibleOptions) emit(cmd *cobra.Command, b []byte) error {
	if o.output == "" {
		_, err := cmd.OutOrStdout().Write(b)
		return err
	}
	// The document carries connection details and may carry credentials, so it is written with
	// the same mode the rest of cargoship writes an inventory with.
	if err := os.WriteFile(o.output, b, 0o600); err != nil {
		return fmt.Errorf("unable to write %s: %w", o.output, err)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Wrote %s\n", o.output)
	return nil
}
