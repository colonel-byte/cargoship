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
	"errors"
	"testing"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/stretchr/testify/require"
)

// withListing swaps the host directory listing for a fixed one for the duration of a test.
func withListing(t *testing.T, listing map[string][]string) {
	t.Helper()

	original := listFiles
	listFiles = func(_ *cluster.ZarfHost, dir string) []string {
		return listing[dir]
	}
	t.Cleanup(func() { listFiles = original })
}

// A file in a managed directory that no registry asks for any more is stale. One that is still
// desired is not, whatever else is in the directory alongside it.
func TestStaleFiles(t *testing.T) {
	withListing(t, map[string][]string{
		registryTLSDir: {
			"/etc/cargoship/tls/removed.example.com.crt",
			"/etc/cargoship/tls/mirror.example.com.crt",
			"/etc/cargoship/tls/also-gone.example.com.crt",
		},
	})

	desired := map[string]DesiredFile{
		"/etc/cargoship/tls/mirror.example.com.crt": {Content: []byte(testCAPEM), Mode: modeConfigFile},
		"/etc/rancher/rke2/registries.yaml":         {Content: []byte("---\n"), Mode: modeRegistries},
	}

	got := StaleFiles(&cluster.ZarfHost{}, []string{registryTLSDir}, desired)

	require.Equal(t, []string{
		"/etc/cargoship/tls/also-gone.example.com.crt",
		"/etc/cargoship/tls/removed.example.com.crt",
	}, got, "only files the desired set no longer names are stale, sorted by path")
}

// A distro with no managed directories has nothing to prune, and neither does an empty listing.
func TestStaleFilesNothingToDo(t *testing.T) {
	withListing(t, map[string][]string{registryTLSDir: nil})

	require.Nil(t, StaleFiles(&cluster.ZarfHost{}, nil, nil), "no managed directories, no stale files")
	require.Nil(t, StaleFiles(&cluster.ZarfHost{}, []string{registryTLSDir}, nil), "an empty directory has no stale files")
}

func TestRemoveStaleFiles(t *testing.T) {
	withListing(t, map[string][]string{
		registryTLSDir: {"/etc/cargoship/tls/gone.example.com.crt", "/etc/cargoship/tls/kept.example.com.crt"},
	})

	fake := &fakeConfigurer{files: map[string]string{
		"/etc/cargoship/tls/gone.example.com.crt": testCAPEM,
		"/etc/cargoship/tls/kept.example.com.crt": testCAPEM,
	}}
	host := &cluster.ZarfHost{Configurer: fake}
	desired := map[string]DesiredFile{
		"/etc/cargoship/tls/kept.example.com.crt": {Content: []byte(testCAPEM), Mode: modeConfigFile},
	}

	require.NoError(t, RemoveStaleFiles(host, []string{registryTLSDir}, desired))
	require.NotContains(t, fake.files, "/etc/cargoship/tls/gone.example.com.crt", "the stale certificate is removed")
	require.Contains(t, fake.files, "/etc/cargoship/tls/kept.example.com.crt", "the desired certificate is left alone")
}

// A file that cannot be deleted is reported rather than swallowed, so it does not come back as
// drift on every run with nothing said about why.
func TestRemoveStaleFilesError(t *testing.T) {
	withListing(t, map[string][]string{registryTLSDir: {"/etc/cargoship/tls/gone.example.com.crt"}})

	host := &cluster.ZarfHost{Configurer: &fakeConfigurer{deleteFileErr: errors.New("permission denied")}}

	err := RemoveStaleFiles(host, []string{registryTLSDir}, nil)
	require.ErrorContains(t, err, "/etc/cargoship/tls/gone.example.com.crt")
}

// The CA directory is managed: cargoship writes every file in it, so it can also remove them.
func TestManagedDirs(t *testing.T) {
	d := &RancherCommon{}
	require.Equal(t, []string{registryTLSDir}, d.ManagedDirs())
}
