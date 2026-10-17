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
	"testing"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/stretchr/testify/require"
)

// TestRemoveSelinuxPolicyOnlyTouchesTargetedHosts is a defect that only shows on an SELinux
// guest: every other phase in a reset takes the target list, and this one did not, so removing a
// single worker unwound the policy cargoship applied on the hosts that were staying.
//
// A reset of a subset hands the phases the whole fleet on purpose -- the node deletions are driven
// through a controller that stays -- so "every host in the configuration" is the wrong set for
// anything that writes.
func TestRemoveSelinuxPolicyOnlyTouchesTargetedHosts(t *testing.T) {
	t.Parallel()

	m := &Manager{
		Config: &cluster.Cluster{
			Spec: cluster.Spec{
				Hosts: cluster.Hosts{
					{
						Hostname: "controller1",
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
		},
	}

	subset := &RemoveSelinuxPolicy{TargetHosts: []string{"worker1"}}
	subset.SetManager(m)

	targets := subset.selinuxTargets()
	require.Len(t, targets, 1, "the policy would have been taken off the hosts that stay")
	require.Equal(t, "worker1", targets[0].Hostname)

	// No target list is a whole-cluster reset, where every host is on its way out anyway.
	whole := &RemoveSelinuxPolicy{}
	whole.SetManager(m)
	require.Len(t, whole.selinuxTargets(), 3)
}
