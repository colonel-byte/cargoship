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

package assemble

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/fileoverride"
	"github.com/stretchr/testify/require"
)

const grabbedPayload = "k3s binary contents\n"

func payloadShasum() string {
	sum := sha256.Sum256([]byte(grabbedPayload))
	return hex.EncodeToString(sum[:])
}

// mirrorServer serves grabbedPayload at every path, standing in for an internal mirror.
func mirrorServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("mirror server write failed: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFileGrabberRemoteOverride(t *testing.T) {
	srv := mirrorServer(t, grabbedPayload)
	buildPath := t.TempDir()

	overrides, err := fileoverride.Parse([]string{"https://rpm.rancher.io=" + srv.URL + "/mirror"})
	require.NoError(t, err)

	file := v1alpha1.ZarfFile{
		Source: "https://rpm.rancher.io/public/k3s.rpm",
		Target: "/usr/local/bin/k3s",
		Shasum: payloadShasum(),
	}

	provenance, err := fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file, overrides)
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(buildPath, string(config.FilesDir), "0", "k3s"))
	require.NoError(t, err)
	require.Equal(t, grabbedPayload, string(got))

	require.NotNil(t, provenance, "an overridden file must report where it actually came from")
	require.Equal(t, distro.FileSource{
		Path:     "files/0/k3s",
		Declared: "https://rpm.rancher.io/public/k3s.rpm",
		Resolved: srv.URL + "/mirror/public/k3s.rpm",
		Override: "https://rpm.rancher.io",
		Shasum:   payloadShasum(),
	}, *provenance)
}

func TestFileGrabberLocalOverride(t *testing.T) {
	staged := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(staged, "public"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(staged, "public", "k3s.rpm"), []byte(grabbedPayload), 0o600))

	buildPath := t.TempDir()
	overrides, err := fileoverride.Parse([]string{"https://rpm.rancher.io=" + staged})
	require.NoError(t, err)

	file := v1alpha1.ZarfFile{
		Source: "https://rpm.rancher.io/public/k3s.rpm",
		Target: "/usr/local/bin/k3s",
		Shasum: payloadShasum(),
	}

	provenance, err := fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file, overrides)
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(buildPath, string(config.FilesDir), "0", "k3s"))
	require.NoError(t, err)
	require.Equal(t, grabbedPayload, string(got))

	require.NotNil(t, provenance)
	require.Equal(t, filepath.Join(staged, "public", "k3s.rpm"), provenance.Resolved,
		"a local override records the path on the build host the bytes were read from")
	require.Equal(t, "files/0/k3s", provenance.Path)
}

// TestFileGrabberRefusesOverrideWithoutShasum covers the one case this feature must not make
// easy: redirecting a download that nothing can verify.
func TestFileGrabberRefusesOverrideWithoutShasum(t *testing.T) {
	srv := mirrorServer(t, grabbedPayload)
	buildPath := t.TempDir()

	overrides, err := fileoverride.Parse([]string{"https://rpm.rancher.io=" + srv.URL})
	require.NoError(t, err)

	file := v1alpha1.ZarfFile{
		Source: "https://rpm.rancher.io/public/k3s.rpm",
		Target: "/usr/local/bin/k3s",
	}

	_, err = fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file, overrides)
	require.ErrorContains(t, err, "declares no shasum")
}

// TestFileGrabberChecksumMismatchLeavesNothingBehind is the regression guard for a staged file
// surviving a failure: buildPath is walked later to generate the package checksums, so anything
// left at dst would ship.
func TestFileGrabberChecksumMismatchLeavesNothingBehind(t *testing.T) {
	srv := mirrorServer(t, "not what the definition declared\n")
	buildPath := t.TempDir()

	overrides, err := fileoverride.Parse([]string{"https://rpm.rancher.io=" + srv.URL})
	require.NoError(t, err)

	file := v1alpha1.ZarfFile{
		Source: "https://rpm.rancher.io/public/k3s.rpm",
		Target: "/usr/local/bin/k3s",
		Shasum: payloadShasum(),
	}

	_, err = fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file, overrides)
	require.Error(t, err)

	_, statErr := os.Stat(filepath.Join(buildPath, string(config.FilesDir), "0", "k3s"))
	require.True(t, os.IsNotExist(statErr), "a file that failed its checksum must not be left in the build path")
}

// TestFileGrabberUnmatchedSourceIsUntouched confirms an override set that does not apply leaves
// the declared source alone rather than guessing.
func TestFileGrabberUnmatchedSourceIsUntouched(t *testing.T) {
	srv := mirrorServer(t, grabbedPayload)
	buildPath := t.TempDir()

	overrides, err := fileoverride.Parse([]string{"https://rpm.rancher.io=https://mirror.invalid"})
	require.NoError(t, err)

	file := v1alpha1.ZarfFile{
		Source: srv.URL + "/k3s",
		Target: "/usr/local/bin/k3s",
		Shasum: payloadShasum(),
	}

	provenance, err := fileGrabber(context.Background(), string(config.FilesDir), buildPath, t.TempDir(), 0, file, overrides)
	require.NoError(t, err)
	require.Nil(t, provenance, "a file no override touched must record nothing, so packages built without overrides are unchanged")

	got, err := os.ReadFile(filepath.Join(buildPath, string(config.FilesDir), "0", "k3s"))
	require.NoError(t, err)
	require.Equal(t, grabbedPayload, string(got))
}

// TestFileGrabberLocalSourceStillResolvesAgainstDistroPath confirms the override plumbing did
// not disturb a definition that already points at a file next to itself.
func TestFileGrabberLocalSourceStillResolvesAgainstDistroPath(t *testing.T) {
	distroPath := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(distroPath, "k3s"), []byte(grabbedPayload), 0o600))

	buildPath := t.TempDir()
	file := v1alpha1.ZarfFile{
		Source: "k3s",
		Target: "/usr/local/bin/k3s",
		Shasum: payloadShasum(),
	}

	_, err := fileGrabber(context.Background(), string(config.FilesDir), buildPath, distroPath, 0, file, nil)
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(buildPath, string(config.FilesDir), "0", "k3s"))
	require.NoError(t, err)
	require.Equal(t, grabbedPayload, string(got))
}
