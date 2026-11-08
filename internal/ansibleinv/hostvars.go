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
	"fmt"
	"sort"
	"strings"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/invopop/jsonschema"
	"github.com/k0sproject/rig/v2/protocol/ssh"
)

// HostVars is the set of Ansible variables cargoship reads for one host, as a type, so that a
// JSON Schema can be generated from it and an editor can complete and check a host_vars or
// group_vars file.
//
// Nothing decodes into it. hostFromVars reads the variables one at a time, because it reports a
// wrong type as the variable that carries it rather than as a path into a document, and that is
// the error an operator can act on. This type exists for the schema and for the test that pins it
// to `known` -- it is a description of the same thing, kept honest by
// TestHostVarsCoverKnownVariables rather than by being the thing itself.
//
// The field types are the real ones, so `cargoship_host` completes as a HostConfig and
// `cargoship_profile_spec` as a Profiles, out of the same $defs the cluster schema publishes.
//
// ansible_port is the one field whose Go type is narrower than what is accepted, and the
// oneof_type tag on it is not decoration. portVar takes a number or a string, because an INI
// inventory gives every variable as a string and a YAML one gives a number, and both are ordinary
// ways to write an inventory -- so the schema has to permit both or it would flag a working file.
// The int here is what portVar returns, not a claim about what it reads.
//
// The port inside cargoship_bastion is deliberately not the same. That variable is decoded whole
// into rig's ssh.Config, whose Port is an int with no such leniency, so a quoted "22" there fails
// the decode with `cannot unmarshal string into Go struct field .port of type int`. The schema
// says integer for it and string-or-integer for ansible_port, which is the asymmetry the code
// actually has rather than a tidier one.
type HostVars struct {
	// Address cargoship opens the SSH connection to. Defaults to the name the inventory knows the host by, which is the common shape.
	AnsibleHost string `json:"ansible_host,omitempty"`
	// Port cargoship connects on. Defaults to 22.
	AnsiblePort int `json:"ansible_port,omitempty" jsonschema:"oneof_type=string;integer"`
	// User cargoship connects as. Defaults to root.
	AnsibleUser string `json:"ansible_user,omitempty"`
	// Path to the SSH private key cargoship authenticates with.
	AnsibleKeyFile string `json:"ansible_ssh_private_key_file,omitempty"`

	// Jump host cargoship reaches this node through. Cargoship does not parse ansible_ssh_common_args, so an inventory that reaches the fleet through a bastion for Ansible's own sake has to say so again here.
	Bastion *ssh.Config `json:"cargoship_bastion,omitempty"`
	// Shell environment cargoship sets on the host, as a map of variable name to value.
	Environment map[string]string `json:"cargoship_environment,omitempty"`
	// Each entry is a file cargoship uploads to the host: src is a path on the management node, dst the path on the node.
	Files []cluster.Files `json:"cargoship_files,omitempty"`
	// Ports cargoship opens on this node, and the firewall rules it applies there.
	Host cluster.HostConfig `json:"cargoship_host,omitempty"`
	// What the node calls itself, when that is not the name the inventory knows it by. Defaults to the inventory hostname.
	Hostname string `json:"cargoship_hostname,omitempty"`
	// Kubernetes node labels applied to this node. A host setting this replaces its group's rather than adding to it.
	NodeLabels map[string]string `json:"cargoship_node_labels,omitempty"`
	// Kubernetes node taints applied to this node. A host setting this replaces its group's rather than adding to it.
	NodeTaints []string `json:"cargoship_node_taints,omitempty"`
	// Address the cluster uses between nodes, when it is not the one cargoship would discover. A node with two interfaces is the usual reason.
	PrivateAddress string `json:"cargoship_private_address,omitempty"`
	// Interface the cluster uses between nodes, when it is not the one cargoship would discover.
	PrivateInterface string `json:"cargoship_private_interface,omitempty"`
	// Name of the profile this host selects. Defaults to the host's role, which is what makes the node-role.kubernetes.io/<profile> label say something.
	Profile string `json:"cargoship_profile,omitempty"`
	// Body of the profile this host selects, declared beside the selection. A profile defined twice with different bodies is an error naming both sources.
	ProfileSpec *cluster.Profiles `json:"cargoship_profile_spec,omitempty"`
}

// JSONSchemaExtend makes the generated schema describe a real host_vars file rather than a
// document cargoship owns.
//
// Two things have to be true at once, and neither is the generator's default. A host_vars file
// carries variables for every role in the play, so an unknown key cannot be an error -- the
// default `additionalProperties: false` would flag every line that is not ours. But a misspelled
// `cargoship_profil` has to be an error, because Ansible has no notion of a variable belonging to
// anyone and a misspelled variable is indistinguishable from an unset one until install time.
//
// The negative lookahead says exactly that: a key under the cargoship_ prefix that is not one of
// the names below matches, and matching a `false` schema is a rejection. Everything else falls
// through to additionalProperties and is allowed. The lookahead is honoured by
// yaml-language-server, which compiles these patterns with JavaScript's own RegExp -- verified
// against the real server rather than assumed, because a pattern it could not compile would fail
// open and silently check nothing.
func (HostVars) JSONSchemaExtend(s *jsonschema.Schema) {
	s.AdditionalProperties = jsonschema.TrueSchema
	if s.PatternProperties == nil {
		s.PatternProperties = map[string]*jsonschema.Schema{}
	}
	s.PatternProperties[unknownPrefixedPattern()] = jsonschema.FalseSchema
}

// unknownPrefixedPattern builds the regular expression matching a cargoship_ variable that is not
// one cargoship reads. It is derived from `known`, so a variable added there is allowed by the
// schema without this being edited.
func unknownPrefixedPattern() string {
	alternatives := make([]string, 0, len(known))
	for _, name := range known {
		alternatives = append(alternatives, strings.TrimPrefix(name, Prefix)+"$")
	}
	// Sorted so the generated schema is byte-identical between runs; `known` is hand-ordered and
	// a reordering there would otherwise show up as a schema diff.
	sort.Strings(alternatives)
	return fmt.Sprintf("^%s(?!%s)", Prefix, strings.Join(alternatives, "|"))
}
