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

// noopUnhold is a passthrough unhold for tests that don't care about unhold behavior.
func noopUnhold(context.Context, *cluster.ZarfHost, []string) error { return nil }

func TestInstallAndPinPackagesForUnholdsInstallsThenHolds(t *testing.T) {
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
	var order []string
	var heldNames, unheldNames []string
	var heldHost *cluster.ZarfHost
	unhold := func(_ context.Context, _ *cluster.ZarfHost, names []string) error {
		order = append(order, "unhold")
		unheldNames = names
		return nil
	}
	hold := func(_ context.Context, hh *cluster.ZarfHost, names []string) error {
		order = append(order, "hold")
		heldHost = hh
		heldNames = names
		return nil
	}

	err := p.installAndPinPackagesFor(context.Background(), byArch, h, fakeNameOf(&nameOfCalls), hold, unhold)

	require.NoError(t, err)
	require.Equal(t, [][]string{{"/tmp/kubelet-arm64.deb"}}, cfg.installed)
	require.Equal(t, []string{"/tmp/kubelet-arm64.deb"}, nameOfCalls)
	require.Equal(t, []string{"pkg-/tmp/kubelet-arm64.deb"}, unheldNames)
	require.Equal(t, []string{"pkg-/tmp/kubelet-arm64.deb"}, heldNames)
	require.Same(t, h, heldHost)
	require.Equal(t, []string{"unhold", "hold"}, order, "unhold must run before install, hold must run after")
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
	unholdCalled := false
	unhold := func(context.Context, *cluster.ZarfHost, []string) error {
		unholdCalled = true
		return nil
	}

	err := p.installAndPinPackagesFor(context.Background(), byArch, h, fakeNameOf(&[]string{}), hold, unhold)

	require.NoError(t, err)
	require.Empty(t, cfg.installed)
	require.False(t, unholdCalled, "a host with no packages for its architecture has nothing to unhold")
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

	err := p.installAndPinPackagesFor(context.Background(), byArch, h, fakeNameOf(&[]string{}), hold, noopUnhold)

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
	unhold := func(context.Context, *cluster.ZarfHost, []string) error {
		t.Fatal("unhold must not run when a package name could not be resolved")
		return nil
	}

	err := p.installAndPinPackagesFor(context.Background(), byArch, h, nameOf, hold, unhold)

	require.ErrorIs(t, err, wantErr)
	require.Empty(t, cfg.installed, "the name lookup runs before unhold and install, so neither happened")
}

func TestInstallAndPinPackagesForPropagatesUnholdError(t *testing.T) {
	p := &UploadFilesCommon{}
	byArch := map[api.Arch][]v1alpha1.ZarfFile{api.ArchAMD64: {{Name: "kubelet-amd64", Target: "/tmp/kubelet-amd64.deb"}}}
	h, cfg := newInstallHost(t, "amd64")

	wantErr := errors.New("apt-mark unhold failed")
	unhold := func(context.Context, *cluster.ZarfHost, []string) error { return wantErr }
	hold := func(context.Context, *cluster.ZarfHost, []string) error {
		t.Fatal("hold must not run when unhold failed")
		return nil
	}

	err := p.installAndPinPackagesFor(context.Background(), byArch, h, fakeNameOf(&[]string{}), hold, unhold)

	require.ErrorIs(t, err, wantErr)
	require.Empty(t, cfg.installed, "the install must not run when unhold failed")
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

	err := p.installAndPinPackagesFor(context.Background(), byArch, h, fakeNameOf(&[]string{}), hold, noopUnhold)

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

func TestUnholdRPMPackagesUsingDeletesWhenVersionlockIsPresent(t *testing.T) {
	h, _ := newInstallHost(t, "amd64")

	var deletedNames []string
	var deletedHost *cluster.ZarfHost
	del := func(hh *cluster.ZarfHost, names []string) error {
		deletedHost = hh
		deletedNames = names
		return nil
	}
	present := func(*cluster.ZarfHost) error { return nil }

	err := unholdRPMPackagesUsing(context.Background(), h, []string{"k3s", "kubelet"}, present, del)

	require.NoError(t, err)
	require.Equal(t, []string{"k3s", "kubelet"}, deletedNames)
	require.Same(t, h, deletedHost)
}

func TestUnholdRPMPackagesUsingPropagatesDeleteError(t *testing.T) {
	h, _ := newInstallHost(t, "amd64")

	wantErr := errors.New("dnf versionlock delete failed")
	del := func(*cluster.ZarfHost, []string) error { return wantErr }
	present := func(*cluster.ZarfHost) error { return nil }

	err := unholdRPMPackagesUsing(context.Background(), h, []string{"k3s"}, present, del)

	require.ErrorIs(t, err, wantErr, "a host that has the plugin and still failed to unhold is a real error")
}

// TestUnholdRPMPackagesUsingSkipsWhenVersionlockIsMissing is the unhold half of
// TestHoldRPMPackagesUsingWarnsWhenVersionlockIsMissing, and it matters more: unhold runs before
// the install, so failing here would fail the whole phase on exactly the hosts that
// docs/agent/choice-unpinnable-hosts.md keeps working.
func TestUnholdRPMPackagesUsingSkipsWhenVersionlockIsMissing(t *testing.T) {
	h, _ := newInstallHost(t, "amd64")

	del := func(*cluster.ZarfHost, []string) error {
		t.Fatal("dnf versionlock delete must not run on a host without the plugin")
		return nil
	}
	present := func(*cluster.ZarfHost) error { return errors.New("No such command: versionlock") }

	var buf bytes.Buffer
	ctx := logger.WithContext(context.Background(), slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))

	err := unholdRPMPackagesUsing(ctx, h, []string{"k3s"}, present, del)

	require.NoError(t, err, "a host with no plugin has nothing locked, so there is nothing to release")
	require.Contains(t, buf.String(), "nothing to release")
}

// TestUnholdRPMPackagesUsingDoesNotWarnWhenVersionlockIsMissing pins the log level: holdRPMPackages
// warns about this same host later in the same phase, and that warning is the one naming a real
// consequence. Two per host per apply would bury it.
func TestUnholdRPMPackagesUsingDoesNotWarnWhenVersionlockIsMissing(t *testing.T) {
	h, _ := newInstallHost(t, "amd64")

	present := func(*cluster.ZarfHost) error { return errors.New("No such command: versionlock") }
	del := func(*cluster.ZarfHost, []string) error { return nil }

	var buf bytes.Buffer
	ctx := logger.WithContext(context.Background(), slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))

	require.NoError(t, unholdRPMPackagesUsing(ctx, h, []string{"k3s"}, present, del))
	require.Empty(t, buf.String(), "the skip is logged at debug, so a warn-level handler sees nothing")
}
