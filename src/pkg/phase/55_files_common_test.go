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
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/src/api"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/stretchr/testify/require"
)

// newDistroFile is a binary-package file for one architecture. An empty arch leaves the selector
// open, which is how a file that applies to every architecture is written.
func newDistroFile(target string, arch api.Arch) *v1alpha1.ZarfFile {
	f := &v1alpha1.ZarfFile{Target: target}
	f.Selector.Package = "binary"
	if arch != "" {
		f.Selector.Arch = api.Arches{arch}
	}
	return f
}

// newFileUploader builds an UploadFilesCommon over the given package files. TempDirectory is a
// scratch directory rather than a real package layout: the selection code only joins paths against
// it, and the chtimes call on a file that is not there logs rather than fails.
func newFileUploader(t *testing.T, files ...*v1alpha1.ZarfFile) *UploadFilesCommon {
	t.Helper()

	p := &UploadFilesCommon{}
	p.SetManager(&Manager{TempDirectory: t.TempDir()})
	p.distroFiles = files

	return p
}

func TestProfileFilesForArchSelectsByArch(t *testing.T) {
	p := newFileUploader(t,
		newDistroFile("/usr/bin/k3s-amd64", api.ArchAMD64),
		newDistroFile("/usr/bin/k3s-arm64", api.ArchARM64),
		newDistroFile("/etc/k3s/config.yaml", ""),
	)

	got := p.profileFilesForArch(context.Background(), "binary", cluster.RoleController, api.ArchARM64)

	require.Equal(t, []string{"k3s-arm64", "config.yaml"}, fileNames(got),
		"an arm64 host takes its own binary and the file that selects no architecture")
}

// TestProfileFilesForArchKeepsTheAssembledIndex is the regression test for the invariant that makes
// architecture selection subtle: a file's position in the package's file list is the name of the
// directory it was assembled into, so skipping one must not renumber the files after it.
func TestProfileFilesForArchKeepsTheAssembledIndex(t *testing.T) {
	p := newFileUploader(t,
		newDistroFile("/usr/bin/first", api.ArchAMD64),
		newDistroFile("/usr/bin/second", api.ArchAMD64),
		newDistroFile("/usr/bin/third", api.ArchARM64),
	)

	got := p.profileFilesForArch(context.Background(), "binary", cluster.RoleController, api.ArchARM64)

	require.Len(t, got, 1)
	require.Equal(t, "2", filepath.Base(filepath.Dir(got[0].LocalSource.Path)),
		"third is the third file in the package, so it was assembled into directory 2 regardless of what was skipped")
}

func TestProfileFilesForArchStillSelectsByProfile(t *testing.T) {
	worker := newDistroFile("/usr/bin/agent", api.ArchAMD64)
	worker.Selector.Profile = cluster.RoleWorker

	p := newFileUploader(t, worker, newDistroFile("/usr/bin/server", api.ArchAMD64))

	require.Equal(t, []string{"server"},
		fileNames(p.profileFilesForArch(context.Background(), "binary", cluster.RoleController, api.ArchAMD64)))
	require.Equal(t, []string{"agent", "server"},
		fileNames(p.profileFilesForArch(context.Background(), "binary", cluster.RoleWorker, api.ArchAMD64)))
}

func TestGetProfileFilesGroupsByTheArchesInTheCluster(t *testing.T) {
	p := newFileUploader(t,
		newDistroFile("/usr/bin/k3s-amd64", api.ArchAMD64),
		newDistroFile("/usr/bin/k3s-arm64", api.ArchARM64),
	)
	p.control = cluster.ZarfHosts{newDetectedHost(t, "amd64")}
	p.workers = cluster.ZarfHosts{newDetectedHost(t, "arm64"), newDetectedHost(t, "arm64")}

	got := p.getProfileFiles(context.Background(), "binary", cluster.RoleController)

	require.Len(t, got, 2, "each architecture in the cluster is keyed once, however many hosts run it")
	require.Equal(t, []string{"k3s-amd64"}, fileNames(got[api.ArchAMD64]))
	require.Equal(t, []string{"k3s-arm64"}, fileNames(got[api.ArchARM64]))
}

// TestGetProfileFilesSkipsAnArchNoHostRuns keeps a package that carries several architectures from
// building file sets for hosts the cluster does not have.
func TestGetProfileFilesSkipsAnArchNoHostRuns(t *testing.T) {
	p := newFileUploader(t,
		newDistroFile("/usr/bin/k3s-amd64", api.ArchAMD64),
		newDistroFile("/usr/bin/k3s-arm64", api.ArchARM64),
	)
	p.control = cluster.ZarfHosts{newDetectedHost(t, "amd64")}

	got := p.getProfileFiles(context.Background(), "binary", cluster.RoleController)

	require.Len(t, got, 1)
	require.Contains(t, got, api.ArchAMD64)
}

func TestFilesForPicksTheHostArchitecture(t *testing.T) {
	p := &UploadFilesCommon{}
	byArch := map[api.Arch][]v1alpha1.ZarfFile{
		api.ArchAMD64: {{Name: "k3s-amd64"}},
		api.ArchARM64: {{Name: "k3s-arm64"}},
	}

	got := p.filesFor(context.Background(), byArch, newDetectedHost(t, "arm64"))

	require.Equal(t, []string{"k3s-arm64"}, fileNames(got))
}

// TestFilesForWithoutAnArchitectureUploadsNothing covers a host that never reported an
// architecture. Uploading another architecture's binaries to it would be worse than uploading none,
// and ValidateHosts has already failed the run for a host the package genuinely cannot serve.
func TestFilesForWithoutAnArchitectureUploadsNothing(t *testing.T) {
	p := &UploadFilesCommon{}
	byArch := map[api.Arch][]v1alpha1.ZarfFile{api.ArchAMD64: {{Name: "k3s-amd64"}}}

	require.Empty(t, p.filesFor(context.Background(), byArch, newDetectedHost(t, "sparc64")))
}

func fileNames(files []v1alpha1.ZarfFile) []string {
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, f.Name)
	}
	return names
}
