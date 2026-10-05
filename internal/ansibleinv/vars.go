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

package ansibleinv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	rig "github.com/k0sproject/rig/v2"
	"github.com/k0sproject/rig/v2/protocol/ssh"
)

// The connection settings rig would apply itself. They are written into the generated document
// rather than left out, because rig's YAML tags carry no omitempty: an unset port is emitted as
// zero, which no schema accepts, rather than omitted.
const (
	defaultSSHUser = "root"
	defaultSSHPort = 22
)

// Prefix is the namespace for the host variables that configure cargoship. A variable under it
// that this package does not know is an error rather than a value ignored, because a misspelled
// variable is indistinguishable from an unset one at install time.
const Prefix = "cargoship_"

// The variables cargoship reads out of the Ansible namespace.
const (
	VarBastion          = Prefix + "bastion"
	VarEnvironment      = Prefix + "environment"
	VarFiles            = Prefix + "files"
	VarHost             = Prefix + "host"
	VarHostname         = Prefix + "hostname"
	VarNodeLabels       = Prefix + "node_labels"
	VarNodeTaints       = Prefix + "node_taints"
	VarPrivateAddress   = Prefix + "private_address"
	VarPrivateInterface = Prefix + "private_interface"
	VarProfile          = Prefix + "profile"
)

// The Ansible connection variables cargoship reads. Everything else under ansible_ is ignored:
// there are hundreds of them, they belong to Ansible's own connection plugins, and cargoship
// does not connect the way Ansible does.
const (
	VarAnsibleHost    = "ansible_host"
	VarAnsiblePort    = "ansible_port"
	VarAnsibleUser    = "ansible_user"
	VarAnsibleKeyFile = "ansible_ssh_private_key_file"
)

// known lists every variable under Prefix that this package reads, for the unknown-variable check.
var known = []string{
	VarBastion,
	VarEnvironment,
	VarFiles,
	VarHost,
	VarHostname,
	VarNodeLabels,
	VarNodeTaints,
	VarPrivateAddress,
	VarPrivateInterface,
	VarProfile,
}

// hostFromVars builds one ZarfHost from the variables Ansible resolved for it.
func hostFromVars(a assignment, vars map[string]any) (*cluster.ZarfHost, error) {
	if err := checkUnknown(a.Host, vars); err != nil {
		return nil, err
	}

	address, err := stringVar(a.Host, vars, VarAnsibleHost)
	if err != nil {
		return nil, err
	}
	if address == "" {
		// An inventory that names a host by its address, which is the common shape, carries no
		// ansible_host at all.
		address = a.Host
	}

	sshCfg := &ssh.Config{Address: address}
	if sshCfg.User, err = stringVar(a.Host, vars, VarAnsibleUser); err != nil {
		return nil, err
	}
	if sshCfg.Port, err = portVar(a.Host, vars, VarAnsiblePort); err != nil {
		return nil, err
	}
	// rig declares defaults for these two and applies them when it loads a document, but its
	// YAML tags carry no omitempty, so an unset field is written out as an empty user and a
	// port of zero rather than left absent. A document that says port 0 is a document that
	// fails its own schema, so the defaults are written here, where they are still rig's.
	if sshCfg.User == "" {
		sshCfg.User = defaultSSHUser
	}
	if sshCfg.Port == 0 {
		sshCfg.Port = defaultSSHPort
	}
	keyPath, err := stringVar(a.Host, vars, VarAnsibleKeyFile)
	if err != nil {
		return nil, err
	}
	if keyPath != "" {
		sshCfg.KeyPath = &keyPath
	}
	if err := decodeVar(a.Host, vars, VarBastion, &sshCfg.Bastion); err != nil {
		return nil, err
	}

	// Hostname defaults to the name the inventory knows the host by. Leaving it unset would let
	// cargoship discover it, but several phases compare the field itself rather than the
	// discovered value, and an inventory in which every host has the same empty name is not one
	// those comparisons survive. cargoship_hostname is the override for an inventory whose names
	// are aliases that do not match the nodes.
	hostname := a.Host
	given, err := stringVar(a.Host, vars, VarHostname)
	if err != nil {
		return nil, err
	}
	if given != "" {
		hostname = given
	}

	host := &cluster.ZarfHost{
		ClientWithConfig: rig.ClientWithConfig{
			ConnectionConfig: rig.CompositeConfig{SSH: sshCfg},
		},
		Hostname: hostname,
		Role:     a.Role,
	}

	if host.Profile, err = stringVar(a.Host, vars, VarProfile); err != nil {
		return nil, err
	}
	if host.Profile == "" {
		// A host with no profile of its own takes one named for its role, which is what makes
		// the node-role.kubernetes.io/<profile> label LabelNodes writes say something.
		host.Profile = a.Role
	}
	if host.PrivateAddress, err = stringVar(a.Host, vars, VarPrivateAddress); err != nil {
		return nil, err
	}
	if host.PrivateInterface, err = stringVar(a.Host, vars, VarPrivateInterface); err != nil {
		return nil, err
	}
	if err := decodeVar(a.Host, vars, VarEnvironment, &host.Environment); err != nil {
		return nil, err
	}
	if err := decodeVar(a.Host, vars, VarFiles, &host.Files); err != nil {
		return nil, err
	}
	if err := decodeVar(a.Host, vars, VarHost, &host.Host); err != nil {
		return nil, err
	}
	if err := decodeVar(a.Host, vars, VarNodeLabels, &host.Engine.NodeLabels); err != nil {
		return nil, err
	}
	if err := decodeVar(a.Host, vars, VarNodeTaints, &host.Engine.NodeTaints); err != nil {
		return nil, err
	}
	return host, nil
}

// checkUnknown refuses a variable under Prefix that cargoship does not read. Ansible has no
// notion of a variable belonging to anyone, so a misspelled cargoship_profil would otherwise sit
// in hostvars looking set and do nothing.
func checkUnknown(host string, vars map[string]any) error {
	var unknown []string
	for name := range vars {
		if !strings.HasPrefix(name, Prefix) {
			continue
		}
		if !contains(known, name) {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("host %q sets %s, which cargoship does not read: expected one of %s",
		host, strings.Join(unknown, ", "), strings.Join(known, ", "))
}

// stringVar reads a variable that has to be a string, or reports the type it found instead.
func stringVar(host string, vars map[string]any, name string) (string, error) {
	value, ok := vars[name]
	if !ok || value == nil {
		return "", nil
	}
	s, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("host %q sets %s to %T, expected a string", host, name, value)
	}
	return s, nil
}

// portVar reads a port, which an INI inventory supplies as a string and a YAML one as a number.
func portVar(host string, vars map[string]any, name string) (int, error) {
	value, ok := vars[name]
	if !ok || value == nil {
		return 0, nil
	}
	switch v := value.(type) {
	case float64:
		// Every JSON number decodes as a float64, so a whole port arrives here.
		if v != float64(int(v)) {
			return 0, fmt.Errorf("host %q sets %s to %v, expected a whole number", host, name, v)
		}
		return int(v), nil
	case int:
		return v, nil
	case string:
		port, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("host %q sets %s to %q, expected a port number", host, name, v)
		}
		return port, nil
	default:
		return 0, fmt.Errorf("host %q sets %s to %T, expected a port number", host, name, value)
	}
}

// decodeVar decodes a structured variable into the field it configures, refusing a key the
// target does not have. The round trip through JSON is what lets an operator write these
// variables in the same vocabulary the inventory schema documents, rather than a second one.
func decodeVar(host string, vars map[string]any, name string, target any) error {
	value, ok := vars[name]
	if !ok || value == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("host %q sets %s to a value cargoship cannot read: %w", host, name, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("host %q sets %s to a value cargoship cannot read: %w", host, name, err)
	}
	return nil
}

// contains reports whether a sorted-or-not string slice holds a value.
func contains(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}
