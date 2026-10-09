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

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
)

// PrepareNodeDelete gives the node's etcd membership up before its Node object is deleted, which
// for kubeadm means running the reset now rather than at uninstall time.
//
// `kubectl delete node` does nothing to the membership here -- there is no etcd controller
// watching node deletion, which is rke2 and k3s's behaviour and not upstream's. The removal is
// kubeadm reset's own `remove-etcd-member` phase, and it needs the local member still voting:
// taking it down first and removing it afterwards is what strands a two-controller cluster on one
// vote of two. Running the reset here is also what makes the deletion stick, because it stops the
// kubelet that would otherwise re-register the node.
//
// leavesOneController changes nothing. The ordering this needs is the ordering that is safe at
// every size, where rke2 and k3s have a last-member case to work around.
//
// UninstallEngine runs PreUninstallReset again before removing the packages. A second reset on a
// host that has already been reset reports nothing to do, and that path is warning-only.
func (d *Upstream) PrepareNodeDelete(ctx context.Context, host *cluster.ZarfHost, _ bool) error {
	return d.PreUninstallReset(ctx, host)
}

// PreUninstallReset runs kubeadm's own teardown before packages are removed, then cleans up what
// kubeadm reset deliberately leaves behind: it prints a reminder about CNI state and iptables/ipvs
// rules rather than removing them itself. Each post-reset command is best-effort -- a link that
// does not exist, or a flannel.1 interface when the cluster runs a different CNI, is the expected
// common case, not a failure worth reporting.
func (d *Upstream) PreUninstallReset(_ context.Context, host *cluster.ZarfHost) error {
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
	return nil
}
