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

package action

import (
	"testing"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/pkg/phase"
	"github.com/stretchr/testify/require"
)

// targetedCluster is a fleet a target list can name: one controller and two workers, each with the
// hostname an operator would write in --target-hosts.
func targetedCluster() *cluster.ZarfCluster {
	return &cluster.ZarfCluster{
		Spec: cluster.ZarfClusterSpec{
			Hosts: cluster.ZarfHosts{
				{
					Hostname: "control1",
					Role:     cluster.RoleController,
				},
				{
					Hostname: "worker1",
					Role:     cluster.RoleWorker,
				},
				{
					Hostname: "worker2",
					Role:     cluster.RoleWorker,
				},
			},
		},
	}
}

// TestNewResetRefusesATargetNothingMatches is the quiet failure this feature could have had: a
// target nobody matches filters every phase down to nothing, so the run reports a reset that
// removed nothing. A typo in a hostname is the ordinary way to get there, and it is caught before
// a single connection is opened.
func TestNewResetRefusesATargetNothingMatches(t *testing.T) {
	reset, err := NewReset(ResetOptions{
		Manager: &phase.Manager{
			DistroID: "k3s",
			Config:   targetedCluster(),
		},
		TargetHosts: []string{"worker1", "worker22"},
	})
	require.Error(t, err)
	require.Nil(t, reset)
	require.Contains(t, err.Error(), "worker22")
	// The message names what the configuration does hold, since the next thing the operator does
	// is look for the name they meant.
	require.Contains(t, err.Error(), "worker2")
	// Only the unmatched target is reported as unmatched; worker1 appears in the message, but as
	// one of the hosts the configuration holds.
	require.Contains(t, err.Error(), "is named worker22:")
}

func TestNewResetWiresTargetHosts(t *testing.T) {
	targets := []string{"worker1"}
	reset, err := NewReset(ResetOptions{
		Manager: &phase.Manager{
			DistroID: "k3s",
			Config:   targetedCluster(),
		},
		TargetHosts: targets,
	})
	require.NoError(t, err)
	require.NotNil(t, reset)

	var foundWorker, foundController, foundUninstall, foundReload bool
	for _, p := range reset.Phases {
		switch phaseObj := p.(type) {
		case *phase.DeleteWorkers:
			require.Equal(t, targets, phaseObj.TargetHosts)
			foundWorker = true
		case *phase.DeleteControllers:
			require.Equal(t, targets, phaseObj.TargetHosts)
			foundController = true
		case *phase.UninstallEngine:
			require.Equal(t, targets, phaseObj.TargetHosts)
			foundUninstall = true
		case *phase.DaemonReload:
			require.Equal(t, targets, phaseObj.TargetHosts)
			foundReload = true
		}
	}
	require.True(t, foundWorker)
	require.True(t, foundController)
	require.True(t, foundUninstall)
	require.True(t, foundReload)
}
