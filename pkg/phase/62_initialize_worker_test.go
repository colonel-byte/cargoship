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

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/stretchr/testify/require"
)

func TestWorkersNeedingInitFallsBackToServiceCheck(t *testing.T) {
	d := fakeServiceDistro{}
	running := newServiceHost("worker-running", false, true)
	stopped := newServiceHost("worker-stopped", false, false)
	controller := newServiceHost("controller-1", true, false)

	got := workersNeedingInit(context.Background(), cluster.ZarfHosts{running, stopped, controller}, d)

	require.Len(t, got, 1)
	require.Equal(t, "worker-stopped", got[0].Hostname)
}

func TestWorkersNeedingInitUsesBootstrapperWhenImplemented(t *testing.T) {
	d := &fakeBootstrapperDistro{bootstrapped: map[string]bool{"worker-done": true}}
	done := &cluster.ZarfHost{Hostname: "worker-done", Role: cluster.RoleWorker}
	pending := &cluster.ZarfHost{Hostname: "worker-pending", Role: cluster.RoleWorker}
	controller := &cluster.ZarfHost{Hostname: "controller-1", Role: cluster.RoleController}

	got := workersNeedingInit(context.Background(), cluster.ZarfHosts{done, pending, controller}, d)

	require.Len(t, got, 1)
	require.Equal(t, "worker-pending", got[0].Hostname)
}
