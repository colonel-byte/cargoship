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
	"encoding/json"
	"testing"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/stretchr/testify/require"
)

// translateOne translates a single controller carrying the given host variables, which is the
// shape every test in this file wants.
func translateOne(t *testing.T, vars map[string]any) (*cluster.ZarfHost, error) {
	t.Helper()
	out, err := Translate(Input{
		Groups:   map[string][]string{"controller": {"kc01"}},
		HostVars: map[string]map[string]any{"kc01": vars},
	}, meta())
	if err != nil {
		return nil, err
	}
	return out.Spec.Hosts[0], nil
}

// asAnsible round trips a value through JSON, so a test sees the types an action plugin
// actually delivers: every number arrives as a float64, whatever it looked like in YAML.
func asAnsible(t *testing.T, vars map[string]any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(vars)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(encoded, &out))
	return out
}

func TestHostFromVarsConnection(t *testing.T) {
	host, err := translateOne(t, asAnsible(t, map[string]any{
		"ansible_host":                 "10.1.2.3",
		"ansible_user":                 "maintainer",
		"ansible_port":                 2222,
		"ansible_ssh_private_key_file": "~/.ssh/id_ed25519",
	}))
	require.NoError(t, err)

	require.Equal(t, "10.1.2.3", host.ConnectionConfig.SSH.Address)
	require.Equal(t, "maintainer", host.ConnectionConfig.SSH.User)
	require.Equal(t, 2222, host.ConnectionConfig.SSH.Port)
	require.NotNil(t, host.ConnectionConfig.SSH.KeyPath)
	require.Equal(t, "~/.ssh/id_ed25519", *host.ConnectionConfig.SSH.KeyPath)
}

func TestHostFromVarsPortTypes(t *testing.T) {
	// An INI inventory gives every variable as a string; a YAML one gives a number. Both are
	// ordinary ways to write an inventory, so both have to work.
	for _, port := range []any{2222, "2222", float64(2222)} {
		host, err := translateOne(t, map[string]any{VarAnsiblePort: port})
		require.NoError(t, err)
		require.Equal(t, 2222, host.ConnectionConfig.SSH.Port)
	}
}

func TestHostFromVarsBadTypes(t *testing.T) {
	tests := []struct {
		name  string
		vars  map[string]any
		wants []string
	}{
		{
			name:  "port is not a number",
			vars:  map[string]any{VarAnsiblePort: "ssh"},
			wants: []string{"kc01", VarAnsiblePort, "expected a port number"},
		},
		{
			name:  "port is fractional",
			vars:  map[string]any{VarAnsiblePort: 22.5},
			wants: []string{VarAnsiblePort, "expected a whole number"},
		},
		{
			name:  "port is a list",
			vars:  map[string]any{VarAnsiblePort: []any{22}},
			wants: []string{VarAnsiblePort, "expected a port number"},
		},
		{
			name:  "user is not a string",
			vars:  map[string]any{VarAnsibleUser: 42},
			wants: []string{VarAnsibleUser, "expected a string"},
		},
		{
			name:  "profile is not a string",
			vars:  map[string]any{VarProfile: []any{"control"}},
			wants: []string{VarProfile, "expected a string"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := translateOne(t, tt.vars)
			require.Error(t, err)
			for _, want := range tt.wants {
				require.Contains(t, err.Error(), want)
			}
		})
	}
}

func TestHostFromVarsCargoshipBeatsAnsible(t *testing.T) {
	// ansible_host is where Ansible connects. cargoship_hostname is what the node calls itself.
	// They are different facts and both are kept.
	host, err := translateOne(t, map[string]any{
		VarAnsibleHost: "10.1.2.3",
		VarHostname:    "distro-kc01",
		VarProfile:     "control",
	})
	require.NoError(t, err)
	require.Equal(t, "10.1.2.3", host.ConnectionConfig.SSH.Address)
	require.Equal(t, "distro-kc01", host.Hostname)
	require.Equal(t, "control", host.Profile)
}

func TestHostFromVarsDefaults(t *testing.T) {
	host, err := translateOne(t, nil)
	require.NoError(t, err)
	// With nothing set, the inventory name is both the address and the hostname, and the role
	// names the profile.
	require.Equal(t, "kc01", host.ConnectionConfig.SSH.Address)
	require.Equal(t, "kc01", host.Hostname)
	require.Equal(t, cluster.RoleController, host.Profile)
	require.Nil(t, host.ConnectionConfig.SSH.KeyPath)
	// rig's own defaults are written out rather than left to rig, because its YAML tags carry
	// no omitempty and an absent field is emitted as a zero one.
	require.Equal(t, "root", host.ConnectionConfig.SSH.User)
	require.Equal(t, 22, host.ConnectionConfig.SSH.Port)
}

func TestHostFromVarsUnknownCargoshipVariableIsRejected(t *testing.T) {
	// A misspelled variable is indistinguishable from an unset one at install time, which is
	// why this is an error rather than a value ignored.
	_, err := translateOne(t, map[string]any{"cargoship_profil": "control"})
	require.ErrorContains(t, err, "cargoship_profil")
	require.ErrorContains(t, err, "which cargoship does not read")
}

func TestHostFromVarsUnknownAnsibleVariableIsIgnored(t *testing.T) {
	// Ansible defines hundreds of these and they are not ours to police.
	host, err := translateOne(t, map[string]any{
		"ansible_python_interpreter": "/usr/bin/python3",
		"ansible_connection":         "ssh",
		"some_site_variable":         "whatever",
	})
	require.NoError(t, err)
	require.Equal(t, "kc01", host.ConnectionConfig.SSH.Address)
}

func TestHostFromVarsStructuredValues(t *testing.T) {
	host, err := translateOne(t, asAnsible(t, map[string]any{
		VarNodeLabels:       map[string]any{"adrp.xyz/purpose-control": "true"},
		VarNodeTaints:       []any{"CriticalOnly=True:NoExecute"},
		VarEnvironment:      map[string]any{"HTTPS_PROXY": "http://proxy:3128"},
		VarPrivateAddress:   "10.9.0.4",
		VarPrivateInterface: "eth1",
		VarHost: map[string]any{
			"ports": []any{map[string]any{"port": "6443", "protocol": "tcp"}},
		},
		VarFiles: []any{map[string]any{
			"name": "registry-ca",
			"src":  "./ca.crt",
			"dst":  "/etc/pki/ca-trust/source/anchors/ca.crt",
			"perm": "0644",
		}},
		VarBastion: map[string]any{"address": "10.0.0.1", "user": "jump", "port": 22},
	}))
	require.NoError(t, err)

	require.Equal(t, "true", host.Engine.NodeLabels["adrp.xyz/purpose-control"])
	require.Equal(t, []string{"CriticalOnly=True:NoExecute"}, host.Engine.NodeTaints)
	require.Equal(t, "http://proxy:3128", host.Environment["HTTPS_PROXY"])
	require.Equal(t, "10.9.0.4", host.PrivateAddress)
	require.Equal(t, "eth1", host.PrivateInterface)

	require.Len(t, host.Host.Ports, 1)
	require.Equal(t, "6443", host.Host.Ports[0].Port)
	require.Equal(t, "tcp", host.Host.Ports[0].Protocol)

	require.Len(t, host.Files, 1)
	require.Equal(t, "registry-ca", host.Files[0].Name)
	require.Equal(t, "/etc/pki/ca-trust/source/anchors/ca.crt", host.Files[0].Destination)

	require.NotNil(t, host.ConnectionConfig.SSH.Bastion)
	require.Equal(t, "10.0.0.1", host.ConnectionConfig.SSH.Bastion.Address)
	require.Equal(t, "jump", host.ConnectionConfig.SSH.Bastion.User)
}

func TestHostFromVarsStructuredValuesRejectUnknownKeys(t *testing.T) {
	// A typo nested inside a structured variable is as silent as one at the top level, so the
	// decoder refuses a key the target does not have.
	tests := []struct {
		name string
		vars map[string]any
		want string
	}{
		{
			name: "file",
			vars: map[string]any{VarFiles: []any{map[string]any{"name": "ca", "destination": "/etc/ca.crt"}}},
			want: "destination",
		},
		{
			name: "host config",
			vars: map[string]any{VarHost: map[string]any{"port": []any{}}},
			want: "port",
		},
		{
			name: "bastion",
			vars: map[string]any{VarBastion: map[string]any{"addr": "10.0.0.1"}},
			want: "addr",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := translateOne(t, tt.vars)
			require.ErrorContains(t, err, tt.want)
			require.ErrorContains(t, err, "kc01")
		})
	}
}

func TestHostFromVarsNullIsUnset(t *testing.T) {
	// An Ansible variable that resolved to nothing is not a value to complain about.
	host, err := translateOne(t, map[string]any{
		VarAnsibleHost: nil,
		VarAnsiblePort: nil,
		VarProfile:     nil,
		VarHost:        nil,
	})
	require.NoError(t, err)
	require.Equal(t, "kc01", host.ConnectionConfig.SSH.Address)
	require.Equal(t, cluster.RoleController, host.Profile)
}
