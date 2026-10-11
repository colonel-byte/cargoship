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

package phase

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/colonel-byte/cargoship/types/distrocfg/registry"
	hostos "github.com/colonel-byte/cargoship/types/os"
	rig "github.com/k0sproject/rig/v2"
	"github.com/k0sproject/rig/v2/protocol/ssh"
	"github.com/stretchr/testify/require"
)

// stoppedConfigurer is a host running no services, which is what makes DeleteCommon.Prepare find
// no leader. Only the two methods the delete phases call on the way there are implemented; the
// embedded interface is nil, so anything else would panic and say so.
type stoppedConfigurer struct {
	hostos.Configurer
}

func (c *stoppedConfigurer) ServiceIsRunning(_ hostos.Host, _ string) bool { return false }

func (c *stoppedConfigurer) Hostname(_ hostos.Host) string { return "node1" }

// deletePhase is what DeleteWorkers and DeleteControllers have in common here.
type deletePhase interface {
	Prepare(context.Context, *cluster.ZarfCluster, *distro.ZarfDistro) error
	ShouldRun() bool
	SetManager(*Manager)
}

// TestDeletePhasesPrepareWithNoLeader covers a panic a live dry run found.
//
// The delete phases find their leader by asking each host whether the controller service is
// running, and DeleteCommon.Prepare warns rather than failing when none is. Both callers then
// built a filter that reached the cluster through that nil leader, so Prepare crashed before
// ShouldRun -- which already returns false on a nil leader -- was ever consulted.
//
// A reset against a cluster with no running controller is an ordinary state: one already reset,
// or one whose engine is down. A dry run guarantees it, because the phases that would have
// started an engine are exactly the ones it skips.
func TestDeletePhasesPrepareWithNoLeader(t *testing.T) {
	builder, err := registry.GetDistroModuleBuilder("rke2")
	require.NoError(t, err)
	dis, ok := builder().(distrocfg.Distro)
	require.True(t, ok)

	for _, tc := range []struct {
		name  string
		phase deletePhase
	}{
		{
			name:  "workers",
			phase: &DeleteWorkers{DeleteCommon: DeleteCommon{Distro: dis}},
		},
		{
			name:  "controllers",
			phase: &DeleteControllers{DeleteCommon: DeleteCommon{Distro: dis}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// One host, so the filter that dereferences the leader actually runs. With an empty
			// host list the closure is never called and the test would pass either way.
			m := &Manager{Config: &cluster.ZarfCluster{Spec: cluster.ZarfClusterSpec{
				Hosts: cluster.ZarfHosts{{Hostname: "node1", Configurer: &stoppedConfigurer{}}},
			}}}
			tc.phase.SetManager(m)

			require.NotPanics(t, func() {
				require.NoError(t, tc.phase.Prepare(context.Background(), m.Config, nil))
			})
			require.False(t, tc.phase.ShouldRun(), "with no leader there is nothing to delete")
		})
	}
}

type fakeRunningServices struct{}

func (fakeRunningServices) ServiceIsRunning(_ context.Context, _ string) bool { return true }
func (fakeRunningServices) StartService(_ context.Context, _ string) error    { return nil }
func (fakeRunningServices) StopService(_ context.Context, _ string) error     { return nil }
func (fakeRunningServices) RestartService(_ context.Context, _ string) error  { return nil }
func (fakeRunningServices) EnableService(_ context.Context, _ string) error   { return nil }

func TestDeleteCommonPrefersNonTargetedLeader(t *testing.T) {
	builder, err := registry.GetDistroModuleBuilder("rke2")
	require.NoError(t, err)
	dis, ok := builder().(distrocfg.Distro)
	require.True(t, ok)

	h1 := &cluster.ZarfHost{
		Hostname: "controller1",
		Role:     cluster.RoleController,
	}
	h1.SetServices(fakeRunningServices{})
	h2 := &cluster.ZarfHost{
		Hostname: "controller2",
		Role:     cluster.RoleController,
	}
	h2.SetServices(fakeRunningServices{})

	m := &Manager{
		Config: &cluster.ZarfCluster{
			Spec: cluster.ZarfClusterSpec{
				Hosts: cluster.ZarfHosts{h1, h2},
			},
		},
	}

	// targeting controller1 should select controller2 as leader
	dc := &DeleteCommon{
		Distro:      dis,
		TargetHosts: []string{"controller1"},
	}
	dc.SetManager(m)

	require.NoError(t, dc.Prepare(context.Background(), m.Config, nil))
	require.NotNil(t, dc.leader)
	require.Equal(t, "controller2", dc.leader.Hostname)
}

// TestMatchesTargetHostUsesTheConfiguredAddress covers the matching an operator's target list is
// held against. ZarfHost.Address reads the live rig client and is empty until the host has been
// dialled, so matching on it would depend on how far through a run the phase is -- and the hosts a
// target list names are exactly the ones a run may not have reached.
func TestMatchesTargetHostUsesTheConfiguredAddress(t *testing.T) {
	host := &cluster.ZarfHost{
		Hostname: "worker3",
		ClientWithConfig: rig.ClientWithConfig{
			ConnectionConfig: rig.CompositeConfig{
				SSH: &ssh.Config{Address: "10.0.0.23"},
			},
		},
	}
	host.Metadata.Hostname = "distro-worker3"

	for _, target := range []string{"worker3", "distro-worker3", "10.0.0.23", "WORKER3"} {
		if !matchesTargetHost(host, []string{target}) {
			t.Errorf("the host was not matched by %q, which is a name an operator could write", target)
		}
	}
	for _, target := range []string{"worker30", "10.0.0.230", ""} {
		if matchesTargetHost(host, []string{target}) {
			t.Errorf("the host was matched by %q", target)
		}
	}
}

// TestCheckTargetHostsNamesWhatItDoesNotKnow pins the message, because the message is the feature:
// a target nobody matches would otherwise filter every phase to nothing and report a reset that
// removed nothing.
func TestCheckTargetHostsNamesWhatItDoesNotKnow(t *testing.T) {
	hosts := cluster.ZarfHosts{
		{Hostname: "control1"},
		{Hostname: "worker1"},
	}

	if err := CheckTargetHosts(hosts, nil); err != nil {
		t.Errorf("an empty target list was refused: %v", err)
	}
	if err := CheckTargetHosts(hosts, []string{"worker1"}); err != nil {
		t.Errorf("a target that names a host was refused: %v", err)
	}

	err := CheckTargetHosts(hosts, []string{"worker1", "worker9"})
	if err == nil {
		t.Fatal("a target naming no host was accepted")
	}
	if !strings.Contains(err.Error(), "worker9") || !strings.Contains(err.Error(), "control1") {
		t.Errorf("the error names neither the target nor the hosts it knows: %v", err)
	}
}

// TestDeleteCommonRefusesWithNoSurvivingController is the case that would otherwise drain and
// delete nodes through a controller the same run is about to uninstall: every running controller
// targeted, but some host kept. Whether that finished would depend on the order the hosts happened
// to be in, which is not a thing to leave to chance on a live cluster.
func TestDeleteCommonRefusesWithNoSurvivingController(t *testing.T) {
	builder, err := registry.GetDistroModuleBuilder("rke2")
	require.NoError(t, err)
	dis, ok := builder().(distrocfg.Distro)
	require.True(t, ok)

	controller := &cluster.ZarfHost{
		Hostname: "controller1",
		Role:     cluster.RoleController,
	}
	controller.SetServices(fakeRunningServices{})
	worker := &cluster.ZarfHost{
		Hostname: "worker1",
		Role:     cluster.RoleWorker,
	}
	worker.SetServices(fakeRunningServices{})

	m := &Manager{
		Config: &cluster.ZarfCluster{
			Spec: cluster.ZarfClusterSpec{
				Hosts: cluster.ZarfHosts{controller, worker},
			},
		},
	}

	// The only controller is targeted and the worker is kept: there is nothing left to drive the
	// node deletions through.
	subset := &DeleteCommon{
		Distro:      dis,
		TargetHosts: []string{"controller1"},
	}
	subset.SetManager(m)
	require.ErrorIs(t, subset.Prepare(context.Background(), m.Config, nil), ErrNoSurvivingController)

	// Targeting every host is a whole-cluster reset, which is exactly what reset is for, so the
	// same controller is allowed to lead its own teardown.
	whole := &DeleteCommon{
		Distro:      dis,
		TargetHosts: []string{"controller1", "worker1"},
	}
	whole.SetManager(m)
	require.NoError(t, whole.Prepare(context.Background(), m.Config, nil))
	require.NotNil(t, whole.leader)
	require.Equal(t, "controller1", whole.leader.Hostname)

	// And no target list at all is the reset that has always existed.
	all := &DeleteCommon{Distro: dis}
	all.SetManager(m)
	require.NoError(t, all.Prepare(context.Background(), m.Config, nil))
	require.NotNil(t, all.leader)
}

func TestDeleteCommonRemovesControllersOneRunAtATime(t *testing.T) {
	builder, err := registry.GetDistroModuleBuilder("rke2")
	require.NoError(t, err)
	dis, ok := builder().(distrocfg.Distro)
	require.True(t, ok)

	hosts := cluster.ZarfHosts{}
	for _, name := range []string{"controller1", "controller2", "controller3"} {
		h := &cluster.ZarfHost{
			Hostname: name,
			Role:     cluster.RoleController,
		}
		h.SetServices(fakeRunningServices{})
		hosts = append(hosts, h)
	}
	m := &Manager{
		Config: &cluster.ZarfCluster{
			Spec: cluster.ZarfClusterSpec{
				Hosts: hosts,
			},
		},
	}

	// Two of three in one run passes through a membership the second removal cannot commit from.
	two := &DeleteCommon{
		Distro:      dis,
		TargetHosts: []string{"controller2", "controller3"},
	}
	two.SetManager(m)
	require.ErrorIs(t, two.Prepare(context.Background(), m.Config, nil), ErrControllersNotOneAtATime)

	// One of three leaves two, so the engine comes down before the node is deleted.
	one := &DeleteCommon{
		Distro:      dis,
		TargetHosts: []string{"controller1"},
	}
	one.SetManager(m)
	require.NoError(t, one.Prepare(context.Background(), m.Config, nil))
	require.Equal(t, "controller2", one.leader.Hostname)
	require.False(t, one.keepEngineForDelete)
}

func TestDeleteCommonKeepsTheEngineUpWhenOneControllerIsLeft(t *testing.T) {
	builder, err := registry.GetDistroModuleBuilder("rke2")
	require.NoError(t, err)
	dis, ok := builder().(distrocfg.Distro)
	require.True(t, ok)

	hosts := cluster.ZarfHosts{}
	for _, name := range []string{"controller1", "controller2"} {
		h := &cluster.ZarfHost{
			Hostname: name,
			Role:     cluster.RoleController,
		}
		h.SetServices(fakeRunningServices{})
		hosts = append(hosts, h)
	}
	m := &Manager{
		Config: &cluster.ZarfCluster{
			Spec: cluster.ZarfClusterSpec{
				Hosts: hosts,
			},
		},
	}

	// The member being removed has to still be up to form the majority of two that commits its
	// own removal, so this is the one case where the engine stays running past the deletion.
	p := &DeleteCommon{
		Distro:      dis,
		TargetHosts: []string{"controller1"},
	}
	p.SetManager(m)
	require.NoError(t, p.Prepare(context.Background(), m.Config, nil))
	require.Equal(t, "controller2", p.leader.Hostname)
	require.True(t, p.keepEngineForDelete)
	require.NoError(t, p.stopEngineBeforeDelete(context.Background(), hosts[0]))
}

// TestMatchNodesToHostsMatchesOnAnyIdentifier is the fix for a host whose machine name is not
// its node name.
//
// The node's name comes from the engine config's `node-name`, written from the inventory's
// hostname, while the machine reports whatever its OS says -- an FQDN on any host with a search
// domain. Probing the reported name asked for `kw1.cargoship.test` when the node was `kw1`,
// which returned not-found for a node that was there: the host was dropped from the phase and
// the engine was uninstalled by a later phase that does no such probe, leaving the node in the
// cluster with nothing left to remove it.
func TestMatchNodesToHostsMatchesOnAnyIdentifier(t *testing.T) {
	t.Parallel()

	listed := "kc0   10.0.2.15,kc0\nkw0   10.0.2.15,kw0\nkw1   10.0.2.15,kw1\n"

	tests := []struct {
		name string
		host *cluster.ZarfHost
		want string
	}{
		{
			name: "the inventory hostname is the node name",
			host: &cluster.ZarfHost{
				Hostname: "kw1",
			},
			want: "kw1",
		},
		{
			name: "the machine reports an fqdn and the inventory holds the short name",
			host: &cluster.ZarfHost{
				Hostname: "kw1",
				Metadata: cluster.ZarfHostMetadata{
					Hostname: "kw1.cargoship.test",
				},
			},
			want: "kw1",
		},
		{
			name: "matched through the private address when no name lines up",
			host: &cluster.ZarfHost{
				Hostname:       "worker-1.internal",
				PrivateAddress: "kw0",
			},
			want: "kw0",
		},
		{
			name: "a host that is not in the cluster matches nothing",
			host: &cluster.ZarfHost{
				Hostname: "kw9",
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := matchNodesToHosts(listed, cluster.ZarfHosts{tt.host})
			require.Equal(t, tt.want, got[tt.host.String()],
				"an absent entry is how the phases tell a host that is in the cluster from one that is not")
		})
	}
}

// TestMatchNodesToHostsClaimsEachNodeOnce covers a fleet whose hosts share an address, which is
// the ordinary case on a multi-homed node: every kubelet there registers the same address, so
// matching on addresses alone would give several hosts the same node.
func TestMatchNodesToHostsClaimsEachNodeOnce(t *testing.T) {
	t.Parallel()

	// Each host carries a connection address, because that is what ZarfHost.String() reports
	// and String() is the key the resolved names are held under. Hosts built without one all
	// stringify alike and would collide in the map rather than in the matching.
	listed := "kc0   10.0.2.15,kc0\nkw0   10.0.2.15,kw0\nkw1   10.0.2.15,kw1\n"
	var hosts cluster.ZarfHosts
	for i, name := range []string{"kc0", "kw0", "kw1"} {
		hosts = append(hosts, &cluster.ZarfHost{
			Hostname: name,
			ClientWithConfig: rig.ClientWithConfig{
				ConnectionConfig: rig.CompositeConfig{
					SSH: &ssh.Config{Address: fmt.Sprintf("10.0.0.%d", 20+i)},
				},
			},
		})
	}

	got := matchNodesToHosts(listed, hosts)
	require.Len(t, got, 3)

	claimed := map[string]string{}
	for host, node := range got {
		require.NotContains(t, claimed, node, "two hosts were given the same node")
		claimed[node] = host
	}
}

// TestDeleteWorkersStopsTheAgentBeforeDeletingTheNode pins the ordering a worker removal needs.
// k3s and RKE2 drop a node when its Node object is deleted, but a kubelet still running
// re-registers it within seconds, so deleting before the engine is down leaves the node behind.
// The assertion is on the order of the phase's own steps, since what went wrong was not any one
// of them failing.
func TestDeleteWorkersStopsTheAgentBeforeDeletingTheNode(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("86_delete_worker.go")
	require.NoError(t, err)

	run := string(src)
	run = run[strings.Index(run, "func (p *DeleteWorkers) Run("):]

	stop := strings.Index(run, "p.stopAgentBeforeDelete")
	del := strings.Index(run, "p.deleteNode")

	require.Positive(t, stop, "the worker removal has to stop the engine before deleting the node")
	require.Positive(t, del)
	require.Less(t, stop, del,
		"the engine must come down before the node is deleted, or the kubelet re-registers it")
}

// TestStopAgentBeforeDeleteIgnoresControllerQuorum covers why this is not the controller helper.
// keepEngineForDelete exists because a controller removal that leaves one controller needs the
// departing member up to commit its own removal. A worker is not an etcd member, so its engine
// comes down whatever that flag says.
func TestStopAgentBeforeDeleteIgnoresControllerQuorum(t *testing.T) {
	t.Parallel()

	builder, err := registry.GetDistroModuleBuilder("rke2")
	require.NoError(t, err)
	dis, ok := builder().(distrocfg.Distro)
	require.True(t, ok)

	worker := &cluster.ZarfHost{
		Hostname: "worker1",
		Role:     cluster.RoleWorker,
	}
	worker.SetServices(fakeRunningServices{})

	p := &DeleteCommon{
		Distro:              dis,
		keepEngineForDelete: true,
	}
	require.NoError(t, p.stopAgentBeforeDelete(context.Background(), worker))
}
