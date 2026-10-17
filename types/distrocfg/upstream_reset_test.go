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

package distrocfg

import (
	"context"
	"testing"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/stretchr/testify/require"
)

// TestPreUninstallResetReachesKubeadmReset confirms PreUninstallReset dispatches to "kubeadm
// reset -f" first. An unconnected test host cannot run SudoExecOutput (the same limitation
// issue #288/#289 hit), so this is as far as dispatch can be observed without a real host.
func TestPreUninstallResetReachesKubeadmReset(t *testing.T) {
	d := &Upstream{}
	h := &cluster.Host{}

	err := d.PreUninstallReset(context.Background(), h)

	require.ErrorContains(t, err, "kubeadm reset")
	require.ErrorIs(t, err, cluster.ErrNotConnected)
}

// TestStopRuntimeTakesContainerdDown is the teardown half of the runtime lifecycle. The start in
// ConfigureEngine is cargoship's, so the stop has to be too: a decommissioned host that keeps
// containerd enabled comes back from a reboot serving a CRI socket for a cluster it has left.
//
// PreUninstallReset itself cannot be driven this far without a connection -- `kubeadm reset` needs
// a real host -- so the step is exercised where it can be.
func TestStopRuntimeTakesContainerdDown(t *testing.T) {
	d := &Upstream{}

	running := &fakeHost{
		serviceRunning:      true,
		allowServiceChanges: true,
	}
	host := running.attach(&cluster.Host{Hostname: "worker-1"})
	require.NoError(t, d.stopRuntime(context.Background(), host))
	require.Equal(t, []string{containerdService}, running.stopped,
		"the runtime was left running on a host the reset removed")

	// A runtime that is already down is not stopped again, which is what makes a re-run of a
	// reset over a half-removed host quiet rather than an error.
	stopped := &fakeHost{allowServiceChanges: true}
	require.NoError(t, d.stopRuntime(context.Background(), stopped.attach(&cluster.Host{Hostname: "worker-1"})))
	require.Empty(t, stopped.stopped)
}
