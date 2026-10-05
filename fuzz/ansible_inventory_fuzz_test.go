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

package fuzz

import (
	"encoding/json"
	"testing"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/internal/ansibleinv"
	goyaml "github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

// seedRequest is a translation request of the shape the action plugin sends: Ansible's own groups
// and hostvars, plus the cluster settings an inventory cannot carry. The targets below mutate it.
const seedRequest = `{
  "groups": {"all": ["kc01", "kc02", "kw01"], "controller": ["kc01", "kc02"], "worker": ["kw01"]},
  "hostvars": {
    "kc01": {"ansible_host": "10.1.2.3", "ansible_user": "ubuntu", "ansible_port": 22, "cargoship_hostname": "distro-kc01"},
    "kc02": {"ansible_host": "10.1.2.4", "ansible_port": "2222"},
    "kw01": {"ansible_host": "10.1.2.5", "cargoship_node_labels": {"disk": "ssd"}}
  },
  "cluster": {"name": "bubbles", "loadbalancer": "bubbles-kc.test.com"}
}`

// FuzzAnsibleRequest fuzzes the whole path a module takes: the JSON an Ansible action plugin
// projects, decoded and translated into the document cargoship installs from.
//
// Every byte of that JSON comes from an inventory the module did not write, so the input is
// attacker-shaped in the ordinary sense -- not hostile, but not checked by anything upstream
// either. A panic here is a module that dies with no JSON on stdout, which Ansible reports as a
// failure of its own devising rather than as the reason.
//
// Two properties hold beyond "does not panic", and both are load-bearing at install time:
//
// The first host is a controller. ConfigureEngine makes the first controller in the document the
// leader, so a translation that emitted a worker first would install a cluster whose leader is a
// worker, and nothing between here and there would say so.
//
// The translation is deterministic. It walks Go maps, and a document that differs between two
// runs of the same inventory is one an operator cannot diff, reproduce, or trust a dry run of.
func FuzzAnsibleRequest(f *testing.F) {
	f.Add(seedRequest)
	f.Add(`{"groups":{"controller":["a"]},"hostvars":{},"cluster":{"name":"c","loadbalancer":"lb"}}`)
	f.Add(`{"groups":{"cp":["a"],"nodes":["b"]},"hostvars":{},"roleGroups":{"controller":["cp"],"worker":["nodes"]},` +
		`"cluster":{"name":"c","loadbalancer":"lb"}}`)
	f.Add(`{"groups":{"controller":["a"],"worker":["a"]},"hostvars":{},"cluster":{"name":"c","loadbalancer":"lb"}}`)
	f.Add(`{"groups":null,"hostvars":null,"cluster":{"name":"c","loadbalancer":"lb"}}`)
	f.Add(`{}`)
	f.Add(``)

	f.Fuzz(func(t *testing.T, document string) {
		req, err := ansibleinv.Decode([]byte(document))
		if err != nil {
			return
		}

		out, err := ansibleinv.Translate(req.Input, req.Cluster)
		if err != nil {
			return
		}

		require.NotEmpty(t, out.Spec.Hosts, "a translation that succeeded emitted no host")
		require.Equal(t, cluster.RoleController, out.Spec.Hosts[0].Role,
			"the first host is the leader, so it has to be a controller")

		for _, host := range out.Spec.Hosts {
			require.NotNil(t, host.ConnectionConfig.SSH, "host %q has no SSH connection", host.Hostname)
			require.NotEmpty(t, host.ConnectionConfig.SSH.Address, "host %q has no address", host.Hostname)
			require.NotZero(t, host.ConnectionConfig.SSH.Port, "host %q has port zero, which no schema accepts", host.Hostname)
			require.NotEmpty(t, host.ConnectionConfig.SSH.User, "host %q has no user", host.Hostname)
			require.NotEmpty(t, host.Role, "a host reached the document with no role")
		}

		again, err := ansibleinv.Translate(req.Input, req.Cluster)
		require.NoError(t, err, "the same input translated once and failed the second time")
		require.Equal(t, mustYAML(t, out), mustYAML(t, again),
			"two translations of one inventory produced different documents")
	})
}

// FuzzAnsibleHostVar fuzzes one host variable at a time against an inventory that is otherwise
// fixed, which is the only way to reach the variable rules with the variable name under the
// fuzzer's control rather than buried in a JSON document it has to keep well-formed.
//
// The rule being checked is the one an operator meets by making a typo. A variable under the
// cargoship_ prefix that cargoship does not read is refused, because Ansible has no notion of a
// variable belonging to anyone and a misspelled cargoship_profil would otherwise sit in hostvars
// looking set and do nothing. Anything outside the prefix that cargoship does not read is
// ignored, and ignored means the generated document is byte-for-byte the one the same inventory
// without that variable produces -- there are hundreds of ansible_ variables and they belong to
// Ansible's connection plugins, not here.
func FuzzAnsibleHostVar(f *testing.F) {
	f.Add("cargoship_", `"bare prefix, no suffix"`)
	f.Add("cargoship_profil", `"controller"`)
	f.Add("cargoship_hostname", `"distro-kc01"`)
	f.Add("cargoship_node_labels", `{"disk": "ssd"}`)
	f.Add("ansible_python_interpreter", `"/usr/bin/python3"`)
	f.Add("ansible_port", `2222`)
	f.Add("ansible_port", `"not a port"`)
	f.Add("ansible_user", `["a list where a string belongs"]`)
	f.Add("", `null`)

	f.Fuzz(func(t *testing.T, name, encoded string) {
		var value any
		if err := json.Unmarshal([]byte(encoded), &value); err != nil {
			// The value is fuzzed as JSON so that the target reaches the types an inventory
			// really carries -- numbers, lists, maps -- and not only strings. Text that is not
			// JSON is used as the string it is.
			value = encoded
		}

		base := ansibleinv.Input{
			Groups:   map[string][]string{"controller": {"kc01"}},
			HostVars: map[string]map[string]any{"kc01": {"ansible_host": "10.1.2.3"}},
		}
		meta := ansibleinv.ClusterMeta{Name: "bubbles", LoadBalancer: "bubbles-kc.test.com"}

		plain, err := ansibleinv.Translate(base, meta)
		require.NoError(t, err, "the fixed inventory this target varies stopped translating")

		withVar := ansibleinv.Input{
			Groups:   base.Groups,
			HostVars: map[string]map[string]any{"kc01": {"ansible_host": "10.1.2.3", name: value}},
		}
		got, err := ansibleinv.Translate(withVar, meta)

		switch {
		case isUnknownCargoshipVar(name):
			require.Error(t, err, "%q is under the cargoship_ prefix and is not read, so it has to be refused", name)
			require.Contains(t, err.Error(), name, "the refusal does not name the variable it refused")
		case isRead(name):
			// A variable cargoship reads may be refused for its type or its contents, and may
			// leave the document unchanged when its value is the one the translation defaults
			// to. What it may not do is produce a host the schema rejects, which Translate
			// checks for itself; the invariants worth restating here are the ones a wrong value
			// could break quietly.
			if err == nil {
				require.Len(t, got.Spec.Hosts, 1, "one host went in and something else came out")
				require.Equal(t, cluster.RoleController, got.Spec.Hosts[0].Role,
					"the one host lost the role its group gave it")
				require.NotZero(t, got.Spec.Hosts[0].ConnectionConfig.SSH.Port, "port zero, which no schema accepts")
			}
		default:
			require.NoError(t, err, "%q is not cargoship's and was not ignored", name)
			require.Equal(t, mustYAML(t, plain), mustYAML(t, got),
				"%q is not cargoship's and changed the generated document", name)
		}
	})
}

// FuzzAnsibleRoleGroups fuzzes the role mapping and the group names it points at, with the hosts
// held to two so that a failure names a shape rather than a crowd.
//
// Role derivation is where an inventory turns into cluster topology, and every way it can be
// wrong is silent: a host in two groups that disagree installed into the wrong plane, a
// misspelled group name that Ansible reports as an empty group and cargoship would read as a
// cluster with no workers, a host counted twice. Each of those is an error rather than a guess,
// and this target holds that true for names no table test would think to write.
func FuzzAnsibleRoleGroups(f *testing.F) {
	f.Add("controller", "worker", "kc01", "kw01")
	f.Add("cp", "nodes", "kc01", "kw01")
	f.Add("controller", "controller", "kc01", "kc01")
	f.Add("missing", "worker", "kc01", "kw01")
	f.Add("", "", "", "")
	f.Add("controller", "worker", "kc01", "kc01")

	f.Fuzz(func(t *testing.T, controlGroup, workerGroup, hostA, hostB string) {
		in := ansibleinv.Input{
			Groups: map[string][]string{
				controlGroup: {hostA},
				workerGroup:  {hostB},
			},
			RoleGroups: map[string][]string{
				cluster.RoleController: {controlGroup},
				cluster.RoleWorker:     {workerGroup},
			},
		}
		meta := ansibleinv.ClusterMeta{Name: "bubbles", LoadBalancer: "bubbles-kc.test.com"}

		out, err := ansibleinv.Translate(in, meta)
		if err != nil {
			return
		}

		// The two groups are one map, so naming the same group twice leaves one entry and one
		// host. Either way, a host is in the document once: a host installed twice is a phase
		// pipeline run against the same node under two roles.
		seen := make(map[string]bool, len(out.Spec.Hosts))
		for _, host := range out.Spec.Hosts {
			require.False(t, seen[host.Hostname], "host %q is in the document twice", host.Hostname)
			seen[host.Hostname] = true
		}

		require.Equal(t, cluster.RoleController, out.Spec.Hosts[0].Role,
			"the first host is the leader, so it has to be a controller")

		// Controllers come before workers. This is the same ordering rule as FuzzAnsibleRequest
		// checks the head of, held over the whole document.
		workers := false
		for _, host := range out.Spec.Hosts {
			if host.Role == cluster.RoleWorker {
				workers = true
				continue
			}
			require.False(t, workers, "controller %q comes after a worker", host.Hostname)
		}
	})
}

// isUnknownCargoshipVar reports whether a variable name is under cargoship's prefix and is not one
// cargoship reads. It is written against the exported names rather than against a copy of the
// list, so a variable added to the package is covered here without this file being touched.
func isUnknownCargoshipVar(name string) bool {
	return len(name) >= len(ansibleinv.Prefix) &&
		name[:len(ansibleinv.Prefix)] == ansibleinv.Prefix &&
		!isRead(name)
}

// isRead reports whether cargoship reads the named variable.
func isRead(name string) bool {
	switch name {
	case ansibleinv.VarBastion, ansibleinv.VarEnvironment, ansibleinv.VarFiles, ansibleinv.VarHost,
		ansibleinv.VarHostname, ansibleinv.VarNodeLabels,
		ansibleinv.VarNodeTaints, ansibleinv.VarPrivateAddress, ansibleinv.VarPrivateInterface,
		ansibleinv.VarProfile, ansibleinv.VarAnsibleHost, ansibleinv.VarAnsiblePort,
		ansibleinv.VarAnsibleUser, ansibleinv.VarAnsibleKeyFile:
		return true
	}
	return false
}

// mustYAML encodes a document the way the translation writes it, which is what two documents are
// compared as. Comparing the structs would compare pointers, and comparing JSON would compare a
// document that is not the one written: several embedded rig types carry omitempty on their JSON
// tags and not on their YAML tags.
func mustYAML(t *testing.T, out *cluster.ZarfCluster) string {
	t.Helper()

	encoded, err := goyaml.Marshal(out)
	require.NoError(t, err)
	return string(encoded)
}
