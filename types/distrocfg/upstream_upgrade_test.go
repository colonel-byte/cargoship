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
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/colonel-byte/cargoship/api"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/types/os/linux"
	"github.com/stretchr/testify/require"
	"github.com/zarf-dev/zarf/src/pkg/logger"
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
//
// The host is Debian-family so this takes the apt path, where the unhold runs unconditionally. On
// the rpm path the versionlock probe runs first, and an unconnected host answers it with an error
// that unholdKubeadm cannot tell apart from an absent plugin, so it would skip the unhold and
// surface the install failure instead. TestUnholdKubeadmUsing* cover that path directly.
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
			h.Configurer = &linux.Ubuntu{}
			h.Metadata.Arch = "amd64"
			h.Metadata.IsLeader = tt.isLeader

			err := d.PreStartUpgrade(context.Background(), h, dis)

			require.ErrorContains(t, err, "unholding kubeadm")
			require.ErrorIs(t, err, cluster.ErrNotConnected)
		})
	}
}

// debugContext collects the debug-level log lines unholdKubeadm writes when it skips a host.
func debugContext() (context.Context, *bytes.Buffer) {
	var buf bytes.Buffer
	ctx := logger.WithContext(context.Background(), slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return ctx, &buf
}

// TestUnholdKubeadmUsingSkipsWhenVersionlockIsMissing is the point of the guard: this unhold runs
// before the install, so failing it on a host with no versionlock plugin would break the upgrade on
// exactly the hosts docs/agent/choice-unpinnable-hosts.md keeps installing.
func TestUnholdKubeadmUsingSkipsWhenVersionlockIsMissing(t *testing.T) {
	h := &cluster.ZarfHost{}
	present := func(*cluster.ZarfHost) error { return errors.New("No such command: versionlock") }
	run := func(*cluster.ZarfHost, string) error {
		t.Fatal("dnf versionlock delete must not run on a host without the plugin")
		return nil
	}

	ctx, buf := debugContext()
	err := unholdKubeadmUsing(ctx, h, config.SelectorRPM, present, run)

	require.NoError(t, err, "a host with no plugin has kubeadm unlocked already, so there is nothing to release")
	require.Contains(t, buf.String(), "nothing to release")
}

// TestUnholdKubeadmUsingDeletesWhenVersionlockIsPresent pins that a host that does have the plugin
// still gets its lock released, which is what lets the new kubeadm install over the old one.
func TestUnholdKubeadmUsingDeletesWhenVersionlockIsPresent(t *testing.T) {
	h := &cluster.ZarfHost{}
	present := func(*cluster.ZarfHost) error { return nil }

	var got []string
	run := func(_ *cluster.ZarfHost, cmd string) error {
		got = append(got, cmd)
		return nil
	}

	ctx, _ := debugContext()
	require.NoError(t, unholdKubeadmUsing(ctx, h, config.SelectorRPM, present, run))
	require.Equal(t, []string{"dnf versionlock delete kubeadm"}, got)
}

// TestUnholdKubeadmUsingPropagatesDeleteError keeps the tolerance narrow: a host that has
// versionlock and still fails to release the lock is a real error.
func TestUnholdKubeadmUsingPropagatesDeleteError(t *testing.T) {
	h := &cluster.ZarfHost{}
	present := func(*cluster.ZarfHost) error { return nil }
	run := func(*cluster.ZarfHost, string) error { return errors.New("locked by another process") }

	ctx, _ := debugContext()
	err := unholdKubeadmUsing(ctx, h, config.SelectorRPM, present, run)

	require.ErrorContains(t, err, "locked by another process")
}

// TestUnholdKubeadmUsingAlwaysUnholdsOnAPT pins that the probe is rpm-only: apt-mark ships in apt
// itself, so a Debian-family host can always release a hold and must not be skipped.
func TestUnholdKubeadmUsingAlwaysUnholdsOnAPT(t *testing.T) {
	h := &cluster.ZarfHost{}
	present := func(*cluster.ZarfHost) error {
		t.Fatal("the versionlock probe must not run on the apt path")
		return nil
	}

	var got []string
	run := func(_ *cluster.ZarfHost, cmd string) error {
		got = append(got, cmd)
		return nil
	}

	ctx, _ := debugContext()
	require.NoError(t, unholdKubeadmUsing(ctx, h, config.SelectorAPT, present, run))
	require.Equal(t, []string{"apt-mark unhold kubeadm"}, got)
}

// TestUnholdKubeadmUsingPropagatesAPTError is the other half of that: nothing excuses a
// Debian-family host from releasing its hold.
func TestUnholdKubeadmUsingPropagatesAPTError(t *testing.T) {
	h := &cluster.ZarfHost{}
	present := func(*cluster.ZarfHost) error { return nil }
	run := func(*cluster.ZarfHost, string) error { return errors.New("dpkg was interrupted") }

	ctx, _ := debugContext()
	err := unholdKubeadmUsing(ctx, h, config.SelectorAPT, present, run)

	require.ErrorContains(t, err, "dpkg was interrupted")
}
