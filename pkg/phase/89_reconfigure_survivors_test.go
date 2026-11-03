// Copyright 2023 k0sctl authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from k0sctl:
// https://github.com/k0sproject/k0sctl
//
// Modifications Copyright 2026 colonel-byte.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//

package phase

import (
	"context"
	"testing"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/stretchr/testify/require"
)

// controllerHosts builds a controller per name, each with a distinct connection address since
// Host.String() reports that and the phase keys off it.
func controllerHosts(names ...string) cluster.Hosts {
	var hosts cluster.Hosts
	for i, name := range names {
		h := &cluster.Host{
			Hostname: name,
			Role:     cluster.RoleController,
		}
		h.SetServices(fakeRunningServices{})
		hosts = append(hosts, h)
		_ = i
	}
	return hosts
}

// TestReconfigureSurvivorsElectsTheFirstHostThatStays pins the election, which has to agree
// with the one ConfigureEngine makes -- control[0] of the hosts it is given -- or an apply
// after the reset would move `server:` again.
func TestReconfigureSurvivorsElectsTheFirstHostThatStays(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		hosts       []string
		targets     []string
		wantLeader  string
		wantRemoved int
		wantRun     bool
	}{
		{
			name:        "the first controller is removed",
			hosts:       []string{"kc0", "kc1", "kc2"},
			targets:     []string{"kc0"},
			wantLeader:  "kc1",
			wantRemoved: 1,
			wantRun:     true,
		},
		{
			name:        "a later controller is removed, so the leader does not move",
			hosts:       []string{"kc0", "kc1", "kc2"},
			targets:     []string{"kc2"},
			wantLeader:  "kc0",
			wantRemoved: 1,
			wantRun:     true,
		},
		{
			name:        "every controller is removed, which is a teardown",
			hosts:       []string{"kc0", "kc1"},
			targets:     []string{"kc0", "kc1"},
			wantRemoved: 2,
			wantRun:     false,
		},
		{
			name:        "no controller is removed",
			hosts:       []string{"kc0", "kc1"},
			targets:     []string{"kw0"},
			wantLeader:  "kc0",
			wantRemoved: 0,
			wantRun:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			hosts := controllerHosts(tt.hosts...)
			m := &Manager{
				Config: &cluster.Cluster{
					Spec: cluster.Spec{
						Hosts: hosts,
					},
				},
			}

			p := &ReconfigureSurvivors{TargetHosts: tt.targets}
			p.SetManager(m)
			require.NoError(t, p.Prepare(context.Background(), m.Config, nil))

			require.Len(t, p.removed, tt.wantRemoved)
			require.Equal(t, tt.wantRun, p.ShouldRun(),
				"a teardown has nothing to repoint, and neither does a run that kept every controller")
			if tt.wantLeader != "" {
				require.NotNil(t, p.leader)
				require.Equal(t, tt.wantLeader, p.leader.Hostname)
			}
		})
	}
}

// TestReconfigureSurvivorsServerForDropsTheKeyOnTheLeader covers the asymmetry that makes a
// host the first controller: it carries no `server:` at all, which is how ConfigureEngine
// writes it. Rewriting the key to point at itself would not be the same thing.
func TestReconfigureSurvivorsServerForDropsTheKeyOnTheLeader(t *testing.T) {
	t.Parallel()

	hosts := controllerHosts("kc0", "kc1", "kc2")
	m := &Manager{
		Config: &cluster.Cluster{
			Spec: cluster.Spec{
				Hosts: hosts,
			},
		},
	}

	p := &ReconfigureSurvivors{TargetHosts: []string{"kc0"}}
	p.SetManager(m)
	require.NoError(t, p.Prepare(context.Background(), m.Config, nil))
	require.Equal(t, "kc1", p.leader.Hostname)

	// Set by Prepare from the leader's configurer, which a fixture host does not carry.
	p.leaderURL = "https://kc1.cargoship.test:9345"

	_, set := p.serverFor(p.leader)
	require.False(t, set, "the first controller names no server")

	other := p.survivors[1]
	require.Equal(t, "kc2", other.Hostname)
	url, set := p.serverFor(other)
	require.True(t, set, "every other controller names the first one")
	require.Equal(t, p.leaderURL, url)
}

// TestReconfigureSurvivorsSkipsADistroWithNoServerKey covers the guard that keeps this phase off
// kubeadm. Its config is a multi-document file holding no `server:`, so the in-place rewrite
// this phase does to an rke2 config.yaml would drop two of the three documents. Doing nothing
// and saying so is the gap tracked in #862; doing it wrong is not a gap but a broken host.
func TestReconfigureSurvivorsSkipsADistroWithNoServerKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		distro  distrocfg.Distro
		wantRun bool
	}{
		{
			name:    "a distro with a server key repoints its survivors",
			distro:  &distrocfg.RKE2{},
			wantRun: true,
		},
		{
			name:    "a distro that bootstraps instead is left alone",
			distro:  &distrocfg.Upstream{},
			wantRun: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			hosts := controllerHosts("kc0", "kc1", "kc2")
			m := &Manager{
				Config: &cluster.Cluster{
					Spec: cluster.Spec{
						Hosts: hosts,
					},
				},
			}

			p := &ReconfigureSurvivors{
				Distro:      tt.distro,
				TargetHosts: []string{"kc0"},
			}
			p.SetManager(m)
			require.NoError(t, p.Prepare(context.Background(), m.Config, nil))

			require.Len(t, p.removed, 1, "the target is a controller, so it is removed either way")
			require.Equal(t, tt.wantRun, p.ShouldRun())
		})
	}
}
