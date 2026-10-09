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
	"errors"
	"testing"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/stretchr/testify/require"
)

// fakePreUninstallResetterDistro is a Distro that implements PreUninstallResetter, matching
// upstream/kubeadm.
type fakePreUninstallResetterDistro struct {
	fakeServiceDistro
	calls []string
	err   error
}

func (f *fakePreUninstallResetterDistro) PreUninstallReset(_ context.Context, h *cluster.ZarfHost) error {
	f.calls = append(f.calls, h.Hostname)
	return f.err
}

func TestPreUninstallResetNoOpWhenNotImplemented(t *testing.T) {
	p := &UninstallEngine{Distro: fakeServiceDistro{}}
	h := &cluster.ZarfHost{Hostname: "node1"}

	require.NoError(t, p.preUninstallReset(context.Background(), h))
}

func TestPreUninstallResetDispatchesWhenImplemented(t *testing.T) {
	d := &fakePreUninstallResetterDistro{}
	p := &UninstallEngine{Distro: d}
	h := &cluster.ZarfHost{Hostname: "node1"}

	require.NoError(t, p.preUninstallReset(context.Background(), h))
	require.Equal(t, []string{"node1"}, d.calls)
}

func TestPreUninstallResetPropagatesError(t *testing.T) {
	wantErr := errors.New("kubeadm reset failed")
	d := &fakePreUninstallResetterDistro{err: wantErr}
	p := &UninstallEngine{Distro: d}
	h := &cluster.ZarfHost{Hostname: "node1"}

	err := p.preUninstallReset(context.Background(), h)

	require.ErrorIs(t, err, wantErr)
}

type fakeBinaryDistro struct {
	fakeServiceDistro
	binary string
}

func (f fakeBinaryDistro) BinaryName() string {
	return f.binary
}

func (f fakeBinaryDistro) PackageStagingDir() string {
	return ""
}

func (f fakeBinaryDistro) CleanupPaths() []string {
	return nil
}

func TestUninstallEngineTargetHostsFiltersHosts(t *testing.T) {
	p := &UninstallEngine{
		Distro:      fakeServiceDistro{},
		TargetHosts: []string{"worker1"},
	}
	m := &Manager{
		Config: &cluster.ZarfCluster{
			Spec: cluster.ZarfClusterSpec{
				Hosts: cluster.ZarfHosts{
					{Hostname: "controller1", Role: cluster.RoleController},
					{Hostname: "worker1", Role: cluster.RoleWorker},
					{Hostname: "worker2", Role: cluster.RoleWorker},
				},
			},
		},
	}
	p.SetManager(m)

	require.NoError(t, p.Prepare(context.Background(), m.Config, nil))
	require.Len(t, p.hosts, 1)
	require.Equal(t, "worker1", p.hosts[0].Hostname)
}

func TestDetectInstalledEnginePackagesNoDistro(t *testing.T) {
	p := &UninstallEngine{Distro: nil}
	h := &cluster.ZarfHost{Hostname: "node1"}
	pkgs := p.detectInstalledEnginePackages(context.Background(), h)
	require.Nil(t, pkgs)
}

func TestDetectInstalledEnginePackagesUnknownDistro(t *testing.T) {
	p := &UninstallEngine{Distro: fakeBinaryDistro{binary: "unknown"}}
	h := &cluster.ZarfHost{Hostname: "node1"}
	pkgs := p.detectInstalledEnginePackages(context.Background(), h)
	require.Nil(t, pkgs)
}

func TestCleanManagedDirsNoDistro(_ *testing.T) {
	p := &UninstallEngine{Distro: nil}
	h := &cluster.ZarfHost{Hostname: "node1"}
	// Should not panic or error with nil distro
	p.cleanManagedDirs(context.Background(), h)
}

// recordingDirsDistro counts the times a distro is asked what it manages on a host.
type recordingDirsDistro struct {
	fakeBinaryDistro
	asked *int
}

func (f recordingDirsDistro) ManagedDirs() []distrocfg.ManagedDir {
	*f.asked++
	return []distrocfg.ManagedDir{{Path: "/etc/cargoship"}}
}

// TestUninstallNodeCleansWhatCargoshipManages is the removal path's half of the cleanup: a host
// marked absent is uninstalled through this same function, so the managed directories -- the TLS
// material, the state directory, the chart manifests -- have to go with the engine rather than
// outliving a node that is no longer in the cluster.
func TestUninstallNodeCleansWhatCargoshipManages(t *testing.T) {
	asked := 0
	p := &UninstallEngine{
		Distro: recordingDirsDistro{
			fakeBinaryDistro: fakeBinaryDistro{binary: "unknown"},
			asked:            &asked,
		},
		TargetHosts: []string{"worker1"},
	}
	h := &cluster.ZarfHost{Hostname: "worker1"}

	require.NoError(t, p.uninstallNode(context.Background(), h))
	require.Positive(t, asked, "the uninstall never asked the distro what it manages")
}
