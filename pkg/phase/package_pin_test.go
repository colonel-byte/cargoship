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

	"github.com/colonel-byte/cargoship/api"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/stretchr/testify/require"
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
		api.ArchAMD64: {{Name: "kubelet-amd64", Target: "/tmp/kubelet-amd64.deb"}},
		api.ArchARM64: {{Name: "kubelet-arm64", Target: "/tmp/kubelet-arm64.deb"}},
	}
	h, cfg := newInstallHost(t, "arm64")

	var nameOfCalls []string
	var heldNames []string
	var heldHost *cluster.ZarfHost
	hold := func(hh *cluster.ZarfHost, names []string) error {
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
	hold := func(*cluster.ZarfHost, []string) error {
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

	hold := func(*cluster.ZarfHost, []string) error {
		t.Fatal("hold must not run when the install never happened")
		return nil
	}

	err := p.installAndPinPackagesFor(context.Background(), byArch, h, fakeNameOf(&[]string{}), hold)

	require.ErrorIs(t, err, api.ErrUnknownArch)
	require.Empty(t, cfg.installed)
}

func TestInstallAndPinPackagesForPropagatesNameLookupError(t *testing.T) {
	p := &UploadFilesCommon{}
	byArch := map[api.Arch][]v1alpha1.ZarfFile{api.ArchAMD64: {{Name: "kubelet-amd64", Target: "/tmp/kubelet-amd64.deb"}}}
	h, cfg := newInstallHost(t, "amd64")

	wantErr := errors.New("dpkg-deb failed")
	nameOf := func(*cluster.ZarfHost, string) (string, error) { return "", wantErr }
	hold := func(*cluster.ZarfHost, []string) error {
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
	hold := func(*cluster.ZarfHost, []string) error { return wantErr }

	err := p.installAndPinPackagesFor(context.Background(), byArch, h, fakeNameOf(&[]string{}), hold)

	require.ErrorIs(t, err, wantErr)
}
