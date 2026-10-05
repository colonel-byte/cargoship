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
