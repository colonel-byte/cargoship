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

	"github.com/colonel-byte/cargoship/api"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/config"
	"github.com/stretchr/testify/require"
)

func kubeadmDistroFiles() v1alpha1.ZarfFiles {
	return v1alpha1.ZarfFiles{
		{Target: "/tmp/kubeadm_amd64.deb", Selector: v1alpha1.BinarySelector{Package: config.SelectorAPT, Arch: api.Arches{api.ArchAMD64}}},
		{Target: "/tmp/kubeadm_arm64.deb", Selector: v1alpha1.BinarySelector{Package: config.SelectorAPT, Arch: api.Arches{api.ArchARM64}}},
		{Target: "/tmp/kubeadm.rpm", Selector: v1alpha1.BinarySelector{Package: config.SelectorRPM, Arch: api.Arches{api.ArchAMD64}}},
		{Target: "/tmp/kubelet_amd64.deb", Selector: v1alpha1.BinarySelector{Package: config.SelectorAPT, Arch: api.Arches{api.ArchAMD64}}},
	}
}

func TestKubeadmPackagePathSelectsAPT(t *testing.T) {
	dis := distro.ZarfDistro{Spec: distro.ZarfDistroSpec{Config: distro.ZarfDistroConfig{OS: distro.ZarfDistroOS{Files: kubeadmDistroFiles()}}}}
	h := &cluster.ZarfHost{}
	h.Metadata.Arch = "amd64"

	path, err := kubeadmPackagePath(dis, h, config.SelectorAPT)

	require.NoError(t, err)
	require.Equal(t, "/tmp/kubeadm_amd64.deb", path)
}

func TestKubeadmPackagePathSelectsRPM(t *testing.T) {
	dis := distro.ZarfDistro{Spec: distro.ZarfDistroSpec{Config: distro.ZarfDistroConfig{OS: distro.ZarfDistroOS{Files: kubeadmDistroFiles()}}}}
	h := &cluster.ZarfHost{}
	h.Metadata.Arch = "amd64"

	path, err := kubeadmPackagePath(dis, h, config.SelectorRPM)

	require.NoError(t, err)
	require.Equal(t, "/tmp/kubeadm.rpm", path)
}

func TestKubeadmPackagePathFiltersByArch(t *testing.T) {
	dis := distro.ZarfDistro{Spec: distro.ZarfDistroSpec{Config: distro.ZarfDistroConfig{OS: distro.ZarfDistroOS{Files: kubeadmDistroFiles()}}}}
	h := &cluster.ZarfHost{}
	h.Metadata.Arch = "arm64"

	path, err := kubeadmPackagePath(dis, h, config.SelectorAPT)

	require.NoError(t, err)
	require.Equal(t, "/tmp/kubeadm_arm64.deb", path)
}

func TestKubeadmPackagePathErrorsWhenNoMatch(t *testing.T) {
	dis := distro.ZarfDistro{Spec: distro.ZarfDistroSpec{Config: distro.ZarfDistroConfig{OS: distro.ZarfDistroOS{Files: kubeadmDistroFiles()}}}}
	h := &cluster.ZarfHost{}
	h.Metadata.Arch = "sparc64"

	_, err := kubeadmPackagePath(dis, h, config.SelectorAPT)

	require.ErrorContains(t, err, "no staged kubeadm package found")
}

func TestPreStartUpgradeErrorsWhenNoKubeadmPackageStaged(t *testing.T) {
	d := &Upstream{}
	dis := distro.ZarfDistro{Spec: distro.ZarfDistroSpec{Config: distro.ZarfDistroConfig{OS: distro.ZarfDistroOS{Files: nil}}}}
	h := &cluster.ZarfHost{}
	h.Metadata.Arch = "amd64"

	err := d.PreStartUpgrade(context.Background(), h, dis)

	require.ErrorContains(t, err, "no staged kubeadm package found")
}

// TestPreStartUpgradeUnholdsBeforeInstall confirms PreStartUpgrade resolves the staged kubeadm
// package and reaches the unhold command first, for both the leader and a non-leader host. An
// unconnected test host cannot run SudoExec/SudoExecOutput (the same limitation issue #288 hit),
// so this is as far as dispatch can be observed without a real host: the leader/non-leader branch
// only diverges after the unhold+install steps that come first, which both hosts reach and fail
// identically on.
func TestPreStartUpgradeUnholdsBeforeInstall(t *testing.T) {
	for _, tt := range []struct {
		name     string
		isLeader bool
	}{
		{name: "leader host", isLeader: true},
		{name: "non-leader host", isLeader: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d := &Upstream{}
			dis := distro.ZarfDistro{Spec: distro.ZarfDistroSpec{Config: distro.ZarfDistroConfig{OS: distro.ZarfDistroOS{Files: kubeadmDistroFiles()}}}}
			h := &cluster.ZarfHost{}
			h.Metadata.Arch = "amd64"
			h.Metadata.IsLeader = tt.isLeader

			err := d.PreStartUpgrade(context.Background(), h, dis)

			require.ErrorContains(t, err, "unholding kubeadm")
			require.ErrorIs(t, err, cluster.ErrNotConnected)
		})
	}
}
