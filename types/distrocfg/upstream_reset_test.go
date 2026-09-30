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

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/stretchr/testify/require"
)

// TestPreUninstallResetReachesKubeadmReset confirms PreUninstallReset dispatches to "kubeadm
// reset -f" first. An unconnected test host cannot run SudoExecOutput (the same limitation
// issue #288/#289 hit), so this is as far as dispatch can be observed without a real host.
func TestPreUninstallResetReachesKubeadmReset(t *testing.T) {
	d := &Upstream{}
	h := &cluster.ZarfHost{}

	err := d.PreUninstallReset(context.Background(), h)

	require.ErrorContains(t, err, "kubeadm reset")
	require.ErrorIs(t, err, cluster.ErrNotConnected)
}
