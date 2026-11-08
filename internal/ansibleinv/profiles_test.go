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

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/stretchr/testify/require"
)

// controlSpec is the profile body the tests below declare, as it arrives from Ansible: a decoded
// YAML document, not a Go struct, because that is what hostvars holds.
func controlSpec() map[string]any {
	return map[string]any{
		"engine": map[string]any{
			"labels": map[string]any{"adrp.xyz/purpose-control": "true"},
		},
	}
}

// controlProfile is the same body as the type it decodes into.
func controlProfile() cluster.Profiles {
	return cluster.Profiles{
		Engine: cluster.HostEngine{
			NodeLabels: map[string]string{"adrp.xyz/purpose-control": "true"},
		},
	}
}

func TestTranslateProfileSpecFromAHost(t *testing.T) {
	out, err := Translate(Input{
		Groups: map[string][]string{
			"controller": {"kc01", "kc02"},
			"worker":     {"kw01"},
		},
		HostVars: map[string]map[string]any{
			// Written once, in group_vars/controller.yml, so both controllers carry it.
			"kc01": {
				VarProfile:     "control",
				VarProfileSpec: controlSpec(),
			},
			"kc02": {
				VarProfile:     "control",
				VarProfileSpec: controlSpec(),
			},
		},
	}, meta())
	require.NoError(t, err)

	require.Equal(t, map[string]cluster.Profiles{"control": controlProfile()}, out.Spec.Config.Profiles)
	require.Equal(t, "control", out.Spec.Hosts[0].Profile)
}

func TestTranslateProfileSpecDefaultsToTheRoleName(t *testing.T) {
	// A host that names no profile takes one named for its role, so a spec on it defines the
	// profile for that whole role. This is the group_vars/worker.yml case.
	out, err := Translate(Input{
		Groups: map[string][]string{
			"controller": {"kc01"},
			"worker":     {"kw01"},
		},
		HostVars: map[string]map[string]any{
			"kw01": {
				VarProfileSpec: map[string]any{"concurrency": "25%"},
			},
		},
	}, meta())
	require.NoError(t, err)

	require.Equal(t, map[string]cluster.Profiles{
		cluster.RoleWorker: {Concurrency: "25%"},
	}, out.Spec.Config.Profiles)
}

func TestTranslateProfileSpecAndClusterBlockTogether(t *testing.T) {
	// The two sources describe different profiles, which is the ordinary case: the playbook keeps
	// the profiles that are nobody's group in particular, the groups keep their own.
	m := meta()
	m.Profiles = map[string]cluster.Profiles{
		"infra": {Engine: cluster.HostEngine{NodeTaints: []string{"adrp.xyz/infra=true:NoSchedule"}}},
	}

	out, err := Translate(Input{
		Groups: map[string][]string{
			"controller": {"kc01"},
			"worker":     {"kw01"},
		},
		HostVars: map[string]map[string]any{
			"kc01": {
				VarProfile:     "control",
				VarProfileSpec: controlSpec(),
			},
			"kw01": {VarProfile: "infra"},
		},
	}, m)
	require.NoError(t, err)

	require.Equal(t, map[string]cluster.Profiles{
		"control": controlProfile(),
		"infra":   {Engine: cluster.HostEngine{NodeTaints: []string{"adrp.xyz/infra=true:NoSchedule"}}},
	}, out.Spec.Config.Profiles)
}

func TestTranslateProfileSpecAgreeingIsNotAConflict(t *testing.T) {
	// The same body written in the cluster block and in group_vars loses nothing either way, so
	// it is allowed. This is what makes a spec safe to repeat while one is being moved.
	m := meta()
	m.Profiles = map[string]cluster.Profiles{"control": controlProfile()}

	out, err := Translate(Input{
		Groups: map[string][]string{"controller": {"kc01"}},
		HostVars: map[string]map[string]any{
			"kc01": {
				VarProfile:     "control",
				VarProfileSpec: controlSpec(),
			},
		},
	}, m)
	require.NoError(t, err)

	require.Equal(t, map[string]cluster.Profiles{"control": controlProfile()}, out.Spec.Config.Profiles)
}

func TestTranslateProfileSpecConflicts(t *testing.T) {
	other := map[string]any{
		"engine": map[string]any{
			"labels": map[string]any{"adrp.xyz/purpose-control": "false"},
		},
	}

	tests := []struct {
		name     string
		declared map[string]cluster.Profiles
		hostVars map[string]map[string]any
		want     string
	}{
		{
			name:     "two hosts disagree",
			declared: nil,
			hostVars: map[string]map[string]any{
				"kc01": {
					VarProfile:     "control",
					VarProfileSpec: controlSpec(),
				},
				"kc02": {
					VarProfile:     "control",
					VarProfileSpec: other,
				},
			},
			want: `profile "control" is defined by host "kc01" and by host "kc02"`,
		},
		{
			name:     "a host disagrees with the cluster block",
			declared: map[string]cluster.Profiles{"control": controlProfile()},
			hostVars: map[string]map[string]any{
				"kc01": {
					VarProfile:     "control",
					VarProfileSpec: other,
				},
			},
			want: `profile "control" is defined by the cluster block and by host "kc01"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := meta()
			m.Profiles = tt.declared

			_, err := Translate(Input{
				Groups:   map[string][]string{"controller": {"kc01", "kc02"}},
				HostVars: tt.hostVars,
			}, m)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestTranslateProfileSpecRejectsAnUnknownKey(t *testing.T) {
	// The spec takes the shape the inventory schema documents for a profile, and a key that shape
	// does not have is a setting silently unset. decodeVar refuses it by name.
	_, err := Translate(Input{
		Groups: map[string][]string{"controller": {"kc01"}},
		HostVars: map[string]map[string]any{
			"kc01": {
				VarProfileSpec: map[string]any{"enginez": map[string]any{}},
			},
		},
	}, meta())
	require.ErrorContains(t, err, VarProfileSpec)
}

func TestTranslateNoProfilesLeavesTheKeyOut(t *testing.T) {
	out, err := Translate(Input{
		Groups: map[string][]string{"controller": {"kc01"}},
	}, meta())
	require.NoError(t, err)
	require.Nil(t, out.Spec.Config.Profiles)
}
