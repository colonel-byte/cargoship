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
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/colonel-byte/cargoship/api"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/stretchr/testify/require"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// fakeNameOf returns a stable, made-up package name per staged file path, recording each call so a
// test can assert which files were resolved and in what order.
func fakeNameOf(calls *[]string) func(*cluster.ZarfHost, string) (string, error) {
	return func(_ *cluster.ZarfHost, path string) (string, error) {
		*calls = append(*calls, path)
		return "pkg-" + path, nil
	}
}

func TestInstallAndPinPackagesForInstallsThenHolds(t *testing.T) {
	p := &UploadFilesCommon{}
	byArch := map[api.Arch][]v1alpha1.ZarfFile{
		api.ArchAMD64: {
			{
				Name:   "kubelet-amd64",
				Target: "/tmp/kubelet-amd64.deb",
			},
		},
		api.ArchARM64: {
			{
				Name:   "kubelet-arm64",
				Target: "/tmp/kubelet-arm64.deb",
			},
		},
	}
	h, cfg := newInstallHost(t, "arm64")

	var nameOfCalls []string
	var heldNames []string
	var heldHost *cluster.ZarfHost
	hold := func(_ context.Context, hh *cluster.ZarfHost, names []string) error {
		heldHost = hh
		heldNames = names
		return nil
	}

	err := p.installAndPinPackagesFor(context.Background(), byArch, h, fakeNameOf(&nameOfCalls), hold)

	require.NoError(t, err)
	require.Equal(t, [][]string{{"/tmp/kubelet-arm64.deb"}}, cfg.installed)
	require.Equal(t, []string{"/tmp/kubelet-arm64.deb"}, nameOfCalls)
	require.Equal(t, []string{"pkg-/tmp/kubelet-arm64.deb"}, heldNames)
	require.Same(t, h, heldHost)
}

func TestInstallAndPinPackagesForSkipsHostWithNoPackages(t *testing.T) {
	p := &UploadFilesCommon{}
	byArch := map[api.Arch][]v1alpha1.ZarfFile{api.ArchAMD64: {{Name: "kubelet-amd64", Target: "/tmp/kubelet-amd64.deb"}}}
	h, cfg := newInstallHost(t, "arm64")

	holdCalled := false
	hold := func(context.Context, *cluster.ZarfHost, []string) error {
		holdCalled = true
		return nil
	}

	err := p.installAndPinPackagesFor(context.Background(), byArch, h, fakeNameOf(&[]string{}), hold)

	require.NoError(t, err)
	require.Empty(t, cfg.installed)
	require.False(t, holdCalled, "a host with no packages for its architecture has nothing to hold")
}

func TestInstallAndPinPackagesForFailsWithoutAnArchitecture(t *testing.T) {
	p := &UploadFilesCommon{}
	byArch := map[api.Arch][]v1alpha1.ZarfFile{api.ArchAMD64: {{Name: "kubelet-amd64", Target: "/tmp/kubelet-amd64.deb"}}}
	h, cfg := newInstallHost(t, "sparc64")

	hold := func(context.Context, *cluster.ZarfHost, []string) error {
		t.Fatal("hold must not run when the install never happened")
		return nil
	}

	err := p.installAndPinPackagesFor(context.Background(), byArch, h, fakeNameOf(&[]string{}), hold)

	require.ErrorIs(t, err, api.ErrUnknownArch)
	require.Empty(t, cfg.installed)
}

// TestInstallAndPinPackagesForPropagatesNameLookupError covers a staged file whose package name
// cannot be read. That is a property of the file, not of the host's pinning tooling, so it fails
// the phase on either package manager rather than quietly leaving the host unpinned.
func TestInstallAndPinPackagesForPropagatesNameLookupError(t *testing.T) {
	p := &UploadFilesCommon{}
	byArch := map[api.Arch][]v1alpha1.ZarfFile{api.ArchAMD64: {{Name: "kubelet-amd64", Target: "/tmp/kubelet-amd64.deb"}}}
	h, cfg := newInstallHost(t, "amd64")

	wantErr := errors.New("dpkg-deb failed")
	nameOf := func(*cluster.ZarfHost, string) (string, error) { return "", wantErr }
	hold := func(context.Context, *cluster.ZarfHost, []string) error {
		t.Fatal("hold must not run when a package name could not be resolved")
		return nil
	}

	err := p.installAndPinPackagesFor(context.Background(), byArch, h, nameOf, hold)

	require.ErrorIs(t, err, wantErr)
	require.Equal(t, [][]string{{"/tmp/kubelet-amd64.deb"}}, cfg.installed, "the install itself already ran before the name lookup failed")
}

func TestInstallAndPinPackagesForPropagatesHoldError(t *testing.T) {
	p := &UploadFilesCommon{}
	byArch := map[api.Arch][]v1alpha1.ZarfFile{api.ArchAMD64: {{Name: "kubelet-amd64", Target: "/tmp/kubelet-amd64.deb"}}}
	h, _ := newInstallHost(t, "amd64")

	wantErr := errors.New("apt-mark hold failed")
	holdCalled := false
	hold := func(context.Context, *cluster.ZarfHost, []string) error {
		holdCalled = true
		return wantErr
	}

	err := p.installAndPinPackagesFor(context.Background(), byArch, h, fakeNameOf(&[]string{}), hold)

	require.ErrorIs(t, err, wantErr)
	require.True(t, holdCalled, "the hold error can only be propagated if hold actually ran")
}

func TestHoldRPMPackagesUsingLocksWhenVersionlockIsPresent(t *testing.T) {
	h, _ := newInstallHost(t, "amd64")

	var lockedNames []string
	var lockedHost *cluster.ZarfHost
	add := func(hh *cluster.ZarfHost, names []string) error {
		lockedHost = hh
		lockedNames = names
		return nil
	}
	present := func(*cluster.ZarfHost) error { return nil }

	err := holdRPMPackagesUsing(context.Background(), h, []string{"k3s", "kubelet"}, present, add)

	require.NoError(t, err)
	require.Equal(t, []string{"k3s", "kubelet"}, lockedNames)
	require.Same(t, h, lockedHost)
}

func TestHoldRPMPackagesUsingPropagatesLockError(t *testing.T) {
	h, _ := newInstallHost(t, "amd64")

	wantErr := errors.New("dnf versionlock add failed")
	add := func(*cluster.ZarfHost, []string) error { return wantErr }
	present := func(*cluster.ZarfHost) error { return nil }

	err := holdRPMPackagesUsing(context.Background(), h, []string{"k3s"}, present, add)

	require.ErrorIs(t, err, wantErr, "a host that has the plugin and still failed to pin is a real error")
}

// TestHoldRPMPackagesUsingWarnsWhenVersionlockIsMissing covers the RHEL-family host that never had
// the versionlock plugin. Cargoship cannot install it over an air gap, and the engine packages are
// already on the host by this point, so the run continues unpinned and says so.
func TestHoldRPMPackagesUsingWarnsWhenVersionlockIsMissing(t *testing.T) {
	h, _ := newInstallHost(t, "amd64")

	add := func(*cluster.ZarfHost, []string) error {
		t.Fatal("dnf versionlock add must not run on a host without the plugin")
		return nil
	}
	present := func(*cluster.ZarfHost) error { return errors.New("No such command: versionlock") }

	var buf bytes.Buffer
	ctx := logger.WithContext(context.Background(), slog.New(slog.NewTextHandler(&buf, nil)))

	err := holdRPMPackagesUsing(ctx, h, []string{"k3s"}, present, add)

	require.NoError(t, err)
	require.Contains(t, buf.String(), "no dnf versionlock plugin", "an unpinned host has to be visible in the log")
	require.Contains(t, buf.String(), "k3s", "the warning names the packages left unpinned")
}
