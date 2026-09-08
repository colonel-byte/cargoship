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
	"testing"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/types/distrocfg"
	"github.com/colonel-byte/cargoship/src/types/distrocfg/registry"
	hostos "github.com/colonel-byte/cargoship/src/types/os"
	rigos "github.com/k0sproject/rig/os"
	"github.com/stretchr/testify/require"
)

// stoppedConfigurer is a host running no services, which is what makes DeleteCommon.Prepare find
// no leader. Only the two methods the delete phases call on the way there are implemented; the
// embedded interface is nil, so anything else would panic and say so.
type stoppedConfigurer struct {
	hostos.Configurer
}

func (c *stoppedConfigurer) ServiceIsRunning(_ rigos.Host, _ string) bool { return false }

func (c *stoppedConfigurer) Hostname(_ rigos.Host) string { return "node1" }

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
		{"workers", &DeleteWorkers{DeleteCommon: DeleteCommon{Distro: dis}}},
		{"controllers", &DeleteControllers{DeleteCommon: DeleteCommon{Distro: dis}}},
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
