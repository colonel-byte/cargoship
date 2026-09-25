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
	"testing"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/stretchr/testify/require"
)

// meta returns the smallest cluster metadata that passes validation, so a test that is about
// something else does not have to say it.
func meta() ClusterMeta {
	return ClusterMeta{Name: "bubbles", LoadBalancer: "bubbles-kc.test.com"}
}

// hostNames returns the generated hosts in document order, which is the order the leader is
// chosen from.
func hostNames(t *testing.T, out *cluster.ZarfCluster) []string {
	t.Helper()
	names := make([]string, 0, len(out.Spec.Hosts))
	for _, h := range out.Spec.Hosts {
		names = append(names, h.Hostname)
	}
	return names
}

func TestTranslateDefaultRoleGroups(t *testing.T) {
	out, err := Translate(Input{
		Groups: map[string][]string{
			"controller": {"kc01", "kc02"},
			"worker":     {"kw01"},
		},
	}, meta())
	require.NoError(t, err)

	require.Equal(t, apiVersion, out.APIVersion)
	require.Equal(t, documentKind, string(out.Kind))
	require.Equal(t, "bubbles", out.Metadata.Name)
	require.Equal(t, "bubbles-kc.test.com", out.Spec.Config.LoadBalancer)
	require.Equal(t, []string{"kc01", "kc02", "kw01"}, hostNames(t, out))

	require.Equal(t, cluster.RoleController, out.Spec.Hosts[0].Role)
	require.Equal(t, cluster.RoleWorker, out.Spec.Hosts[2].Role)
	// A host with no address of its own is reached at the name the inventory knows it by.
	require.Equal(t, "kc01", out.Spec.Hosts[0].SSH.Address)
	// A host with no profile of its own takes one named for its role.
	require.Equal(t, cluster.RoleController, out.Spec.Hosts[0].Profile)
}

func TestTranslateControllersComeFirst(t *testing.T) {
	// Workers are listed first in the mapping and first in the inventory. The leader is the
	// first host of the document, so controllers still have to come out first.
	out, err := Translate(Input{
		Groups: map[string][]string{
			"drones": {"kw01", "kw02"},
			"brains": {"kc01"},
		},
		RoleGroups: map[string][]string{
			cluster.RoleWorker:     {"drones"},
			cluster.RoleController: {"brains"},
		},
	}, meta())
	require.NoError(t, err)
	require.Equal(t, []string{"kc01", "kw01", "kw02"}, hostNames(t, out))
	require.Equal(t, cluster.RoleController, out.Spec.Hosts[0].Role)
}

func TestTranslateGroupOrderIsKept(t *testing.T) {
	// Two groups map to one role. Inventory order inside a group, and mapping order between
	// groups, both survive: the leader is a fact about position.
	out, err := Translate(Input{
		Groups: map[string][]string{
			"primary":   {"kc02", "kc01"},
			"secondary": {"kc03"},
			"worker":    {"kw01"},
		},
		RoleGroups: map[string][]string{
			cluster.RoleController: {"primary", "secondary"},
			cluster.RoleWorker:     {"worker"},
		},
	}, meta())
	require.NoError(t, err)
	require.Equal(t, []string{"kc02", "kc01", "kc03", "kw01"}, hostNames(t, out))
}

func TestTranslateUngroupedHostIsExcluded(t *testing.T) {
	// A play's inventory may carry hosts that have nothing to do with the cluster.
	out, err := Translate(Input{
		Groups: map[string][]string{
			"controller": {"kc01"},
			"worker":     {"kw01"},
			"databases":  {"db01"},
			"all":        {"kc01", "kw01", "db01"},
		},
	}, meta())
	require.NoError(t, err)
	require.Equal(t, []string{"kc01", "kw01"}, hostNames(t, out))
}

func TestTranslateHostInOneRoleTwiceIsKeptOnce(t *testing.T) {
	out, err := Translate(Input{
		Groups: map[string][]string{
			"primary": {"kc01"},
			"spare":   {"kc01"},
			"worker":  {"kw01"},
		},
		RoleGroups: map[string][]string{
			cluster.RoleController: {"primary", "spare"},
			cluster.RoleWorker:     {"worker"},
		},
	}, meta())
	require.NoError(t, err)
	require.Equal(t, []string{"kc01", "kw01"}, hostNames(t, out))
}

func TestTranslateRoleErrors(t *testing.T) {
	tests := []struct {
		name  string
		in    Input
		wants []string
	}{
		{
			name: "host takes two roles",
			in: Input{Groups: map[string][]string{
				"controller": {"kc01"},
				"worker":     {"kc01"},
			}},
			wants: []string{"kc01", "controller", "worker", "one role"},
		},
		{
			name: "no controller",
			in: Input{Groups: map[string][]string{
				"controller": {},
				"worker":     {"kw01"},
			}},
			wants: []string{"no host takes the controller role"},
		},
		{
			name: "no host at all",
			in: Input{Groups: map[string][]string{
				"controller": {},
				"worker":     {},
			}},
			wants: []string{"nothing to install"},
		},
		{
			name: "mapping names a group the inventory does not define",
			in: Input{
				Groups:     map[string][]string{"controller": {"kc01"}},
				RoleGroups: map[string][]string{cluster.RoleController: {"brains"}},
			},
			wants: []string{"brains", "does not define"},
		},
		{
			name: "mapping names a role cargoship does not have",
			in: Input{
				Groups:     map[string][]string{"etcd": {"kc01"}},
				RoleGroups: map[string][]string{"etcd": {"etcd"}},
			},
			wants: []string{`role "etcd"`, "controller, worker"},
		},
		{
			name: "mapping names a role with no groups",
			in: Input{
				Groups:     map[string][]string{"controller": {"kc01"}},
				RoleGroups: map[string][]string{cluster.RoleController: {}},
			},
			wants: []string{"with no groups"},
		},
		{
			// Found by FuzzAnsibleRequest. An empty host name became the address, the hostname
			// and the key into hostvars, and the schema took all three because each is a string.
			name: "group lists a host with an empty name",
			in: Input{
				Groups: map[string][]string{"controller": {""}},
			},
			wants: []string{`group "controller"`, "empty name"},
		},
		{
			name: "mapping names a group with an empty name",
			in: Input{
				Groups:     map[string][]string{"controller": {"kc01"}},
				RoleGroups: map[string][]string{cluster.RoleController: {""}},
			},
			wants: []string{"role controller", "empty name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Translate(tt.in, meta())
			require.Error(t, err)
			for _, want := range tt.wants {
				require.Contains(t, err.Error(), want)
			}
		})
	}
}

func TestTranslateClusterMetaIsRequired(t *testing.T) {
	in := Input{Groups: map[string][]string{"controller": {"kc01"}}}

	_, err := Translate(in, ClusterMeta{LoadBalancer: "bubbles-kc.test.com"})
	require.ErrorContains(t, err, "cluster name is required")

	_, err = Translate(in, ClusterMeta{Name: "bubbles"})
	require.ErrorContains(t, err, "loadbalancer is required")
}

func TestTranslateValidatesAgainstTheSchema(t *testing.T) {
	// The cluster name is what the kubeconfig context is called, and the schema constrains it.
	// A translation that produced an invalid document has to say so here, not ten minutes into
	// an apply.
	_, err := Translate(
		Input{Groups: map[string][]string{"controller": {"kc01"}}},
		ClusterMeta{Name: "Bubbles Cluster", LoadBalancer: "bubbles-kc.test.com"},
	)
	require.ErrorContains(t, err, "metadata.name")
}

func TestTranslateCarriesClusterMetaThrough(t *testing.T) {
	out, err := Translate(
		Input{Groups: map[string][]string{"controller": {"kc01"}}},
		ClusterMeta{
			Name:         "bubbles",
			LoadBalancer: "bubbles-kc.test.com",
			Profiles: map[string]cluster.ZarfClusterProfiles{
				"control": {Engine: cluster.ZarfHostEngine{NodeLabels: map[string]string{"adrp.xyz/purpose-control": "true"}}},
			},
			Values: map[string]any{"addons": map[string]any{"disabled": []any{"rke2-ingress-nginx"}}},
		},
	)
	require.NoError(t, err)
	require.Contains(t, out.Spec.Config.Profiles, "control")
	require.Equal(t, "true", out.Spec.Config.Profiles["control"].Engine.NodeLabels["adrp.xyz/purpose-control"])
	require.Contains(t, out.Spec.Config.Values, "addons")
}
