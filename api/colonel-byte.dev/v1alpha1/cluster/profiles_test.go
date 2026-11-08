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

package cluster

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveProfilesFillsInFromTheParent(t *testing.T) {
	out, err := ResolveProfiles(map[string]Profiles{
		"base": {
			Host:        HostConfig{Ports: []HostPort{{Port: "6443", Protocol: "tcp"}}},
			Engine:      HostEngine{NodeLabels: map[string]string{"tier": "base"}},
			Concurrency: "25%",
		},
		"infra": {
			Inherit: "base",
			Engine:  HostEngine{NodeTaints: []string{"adrp.xyz/infra=true:NoSchedule"}},
		},
	})
	require.NoError(t, err)

	// infra said nothing about ports or concurrency, so base's survive.
	require.Equal(t, []HostPort{{Port: "6443", Protocol: "tcp"}}, out["infra"].Host.Ports)
	require.Equal(t, "25%", out["infra"].Concurrency)
	require.Equal(t, []string{"adrp.xyz/infra=true:NoSchedule"}, out["infra"].Engine.NodeTaints)
	// Labels are a whole field, and infra set none, so base's are taken entire.
	require.Equal(t, map[string]string{"tier": "base"}, out["infra"].Engine.NodeLabels)
	// The chain is folded in, so nothing downstream sees an unresolved inherit.
	require.Empty(t, out["infra"].Inherit)
	// The parent is untouched.
	require.Equal(t, "25%", out["base"].Concurrency)
}

func TestResolveProfilesChildWins(t *testing.T) {
	out, err := ResolveProfiles(map[string]Profiles{
		"base": {
			Engine:      HostEngine{NodeLabels: map[string]string{"tier": "base"}},
			Concurrency: "25%",
		},
		"control": {
			Inherit:     "base",
			Engine:      HostEngine{NodeLabels: map[string]string{"tier": "control"}},
			Concurrency: "1",
		},
	})
	require.NoError(t, err)

	require.Equal(t, map[string]string{"tier": "control"}, out["control"].Engine.NodeLabels)
	require.Equal(t, "1", out["control"].Concurrency)
}

func TestResolveProfilesWalksAChain(t *testing.T) {
	out, err := ResolveProfiles(map[string]Profiles{
		"a": {Concurrency: "50%", Engine: HostEngine{NodeLabels: map[string]string{"from": "a"}}},
		"b": {Inherit: "a", Host: HostConfig{Ports: []HostPort{{Port: "80", Protocol: "tcp"}}}},
		"c": {Inherit: "b", Engine: HostEngine{NodeTaints: []string{"t=1:NoSchedule"}}},
	})
	require.NoError(t, err)

	// c takes its ports from b and everything else from a, two links up.
	require.Equal(t, []HostPort{{Port: "80", Protocol: "tcp"}}, out["c"].Host.Ports)
	require.Equal(t, "50%", out["c"].Concurrency)
	require.Equal(t, map[string]string{"from": "a"}, out["c"].Engine.NodeLabels)
}

func TestResolveProfilesNearestAncestorWins(t *testing.T) {
	out, err := ResolveProfiles(map[string]Profiles{
		"a": {Concurrency: "100%"},
		"b": {Inherit: "a", Concurrency: "50%"},
		"c": {Inherit: "b"},
	})
	require.NoError(t, err)
	require.Equal(t, "50%", out["c"].Concurrency)
}

func TestResolveProfilesErrors(t *testing.T) {
	tests := []struct {
		name     string
		profiles map[string]Profiles
		want     string
	}{
		{
			name: "inherits a profile that does not exist",
			profiles: map[string]Profiles{
				"infra": {Inherit: "bace"},
			},
			want: `profile "infra" inherits "bace", which the cluster does not define`,
		},
		{
			name: "inherits itself",
			profiles: map[string]Profiles{
				"loop": {Inherit: "loop"},
			},
			want: `profile "loop" inherits in a cycle`,
		},
		{
			name: "two profiles inherit each other",
			profiles: map[string]Profiles{
				"a": {Inherit: "b"},
				"b": {Inherit: "a"},
			},
			want: "inherits in a cycle",
		},
		{
			name: "a longer cycle",
			profiles: map[string]Profiles{
				"a": {Inherit: "b"},
				"b": {Inherit: "c"},
				"c": {Inherit: "a"},
			},
			want: "a -> b -> c -> a",
		},
		{
			name: "a broken link further up the chain",
			profiles: map[string]Profiles{
				"a": {Inherit: "missing"},
				"b": {Inherit: "a"},
			},
			want: `profile "a" inherits "missing", which the cluster does not define`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolveProfiles(tt.profiles)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestResolveProfilesLeavesAPlainMapAlone(t *testing.T) {
	in := map[string]Profiles{
		"control": {Concurrency: "1"},
		"worker":  {},
	}
	out, err := ResolveProfiles(in)
	require.NoError(t, err)
	require.Equal(t, in, out)
}

func TestResolveProfilesEmpty(t *testing.T) {
	out, err := ResolveProfiles(nil)
	require.NoError(t, err)
	require.Nil(t, out)
}
