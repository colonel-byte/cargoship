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
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/stretchr/testify/require"
)

// fakeServiceState is a HostServices double that reports a fixed running state, for testing the
// non-Bootstrapper fallback path of controllersNeedingInit/workersNeedingInit.
type fakeServiceState struct {
	running bool
}

func (f *fakeServiceState) ServiceIsRunning(context.Context, string) bool { return f.running }
func (f *fakeServiceState) StartService(context.Context, string) error    { return nil }
func (f *fakeServiceState) StopService(context.Context, string) error     { return nil }
func (f *fakeServiceState) RestartService(context.Context, string) error  { return nil }
func (f *fakeServiceState) EnableService(context.Context, string) error   { return nil }

// fakeServiceDistro is a Distro that does not implement Bootstrapper, matching rke2/k3s.
type fakeServiceDistro struct {
	distrocfg.Distro
}

func (fakeServiceDistro) GetControllerService() string { return "control-service" }
func (fakeServiceDistro) GetWorkerService() string     { return "worker-service" }

// fakeBootstrapperDistro is a Distro that implements Bootstrapper, matching upstream/kubeadm.
type fakeBootstrapperDistro struct {
	distrocfg.Distro
	bootstrapped   map[string]bool
	bootstrapCalls []string
	bootstrapErr   error
}

func (f *fakeBootstrapperDistro) IsBootstrapped(h *cluster.ZarfHost) bool {
	return f.bootstrapped[h.Hostname]
}

func (f *fakeBootstrapperDistro) Bootstrap(_ context.Context, h *cluster.ZarfHost, _ cluster.ZarfRuntimeMeta, _ distro.ZarfDistro) error {
	f.bootstrapCalls = append(f.bootstrapCalls, h.Hostname)
	return f.bootstrapErr
}

func newServiceHost(hostname string, controller bool, running bool) *cluster.ZarfHost {
	role := cluster.RoleWorker
	if controller {
		role = cluster.RoleController
	}
	h := &cluster.ZarfHost{Hostname: hostname, Role: role}
	h.Metadata.DistroVersion = UnknownVersion
	h.SetServices(&fakeServiceState{running: running})
	return h
}

func TestControllersNeedingInitFallsBackToServiceCheck(t *testing.T) {
	d := fakeServiceDistro{}
	running := newServiceHost("controller-running", true, true)
	stopped := newServiceHost("controller-stopped", true, false)
	worker := newServiceHost("worker-1", false, false)

	got := controllersNeedingInit(context.Background(), cluster.ZarfHosts{running, stopped, worker}, d)

	require.Len(t, got, 1)
	require.Equal(t, "controller-stopped", got[0].Hostname)
}

func TestControllersNeedingInitUsesBootstrapperWhenImplemented(t *testing.T) {
	d := &fakeBootstrapperDistro{bootstrapped: map[string]bool{"controller-done": true}}
	done := &cluster.ZarfHost{Hostname: "controller-done", Role: cluster.RoleController}
	pending := &cluster.ZarfHost{Hostname: "controller-pending", Role: cluster.RoleController}
	worker := &cluster.ZarfHost{Hostname: "worker-1", Role: cluster.RoleWorker}

	got := controllersNeedingInit(context.Background(), cluster.ZarfHosts{done, pending, worker}, d)

	require.Len(t, got, 1)
	require.Equal(t, "controller-pending", got[0].Hostname)
}
