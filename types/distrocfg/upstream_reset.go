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
	"fmt"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
)

// PreUninstallReset runs kubeadm's own teardown before packages are removed, then cleans up what
// kubeadm reset deliberately leaves behind: it prints a reminder about CNI state and iptables/ipvs
// rules rather than removing them itself. Each post-reset command is best-effort -- a link that
// does not exist, or a flannel.1 interface when the cluster runs a different CNI, is the expected
// common case, not a failure worth reporting.
//
// The container runtime goes last. `kubeadm reset` needs it to tear the control-plane containers
// down, so it is stopped only once that has run -- and it is stopped at all because nothing else
// on an upstream host started it: leaving it enabled means a decommissioned machine still comes
// back with a runtime serving a CRI socket for a cluster it is no longer part of.
func (d *Upstream) PreUninstallReset(ctx context.Context, host *cluster.Host) error {
	if _, err := host.SudoExecOutput("kubeadm reset -f"); err != nil {
		return fmt.Errorf("kubeadm reset: %w", err)
	}

	for _, cmd := range []string{
		"ip link delete cni0",
		"ip link delete flannel.1",
		"iptables -F",
		"iptables -t nat -F",
		"iptables -t mangle -F",
		"iptables -X",
	} {
		_ = host.SudoExec(cmd) //nolint:errcheck // best-effort teardown; these fail on a host that never came fully up
	}

	return d.stopRuntime(ctx, host)
}

// stopRuntime takes the container runtime back down, which is the counterpart of the start in
// ConfigureEngine: nothing else on an upstream host brought it up, so nothing else will stop it.
//
// Disabling has no typed equivalent on a host, and it is the same shape of best-effort cleanup as
// the commands above -- a runtime that was never enabled is the ordinary case on a host that never
// came fully up -- so only the stop is reported.
func (d *Upstream) stopRuntime(ctx context.Context, host *cluster.Host) error {
	if host.ServiceIsRunning(ctx, containerdService) {
		if err := host.StopService(ctx, containerdService); err != nil {
			return fmt.Errorf("stopping %s: %w", containerdService, err)
		}
	}
	_ = host.SudoExec("systemctl disable " + containerdService) //nolint:errcheck // best-effort

	return nil
}
