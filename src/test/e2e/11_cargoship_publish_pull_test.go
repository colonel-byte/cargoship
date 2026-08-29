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

package test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/src/test"
	"github.com/stretchr/testify/require"
)

// TestCargoshipPublishPullRoundTrip exercises the built cargoship binary's "publish" and
// "pull" commands end-to-end against an in-memory OCI registry, avoiding any dependency on
// a real registry or network access for the OCI transport itself.
func TestCargoshipPublishPullRoundTrip(t *testing.T) {
	t.Setenv("CARGOSHIP_CONFIG", "src/test/e2e/cargoship-config.yaml")

	createDir := t.TempDir()
	_, _, err := e2e.Cargoship(t, "--no-color", "create", "example/rke2/v1.35.0-rke2r1", "-o", createDir)
	require.NoError(t, err)

	published, err := filepath.Glob(filepath.Join(createDir, "*.tar.zst"))
	require.NoError(t, err)
	require.Len(t, published, 1, "expected exactly one package produced by create")
	pkgPath := published[0]

	addr := test.SetupInMemoryRegistry(t)

	// Publish always tags the package as "<repository>/<metadata.name>:<metadata.version>",
	// regardless of the reference passed on the CLI, so the destination below resolves to
	// "<addr>/e2e-test/rancher-rke2:1.35.0-rke2r1".
	const (
		packageName    = "rancher-rke2"
		packageVersion = "1.35.0-rke2r1"
	)
	dst := fmt.Sprintf("oci://%s/e2e-test", addr)
	src := fmt.Sprintf("oci://%s/e2e-test/%s:%s", addr, packageName, packageVersion)

	_, _, err = e2e.Cargoship(t, "--no-color", "publish", pkgPath, dst, "--plain-http", "--confirm")
	require.NoError(t, err)

	pullDir := t.TempDir()
	_, _, err = e2e.Cargoship(t, "--no-color", "pull", src, "--plain-http", "-o", pullDir)
	require.NoError(t, err)

	pulled, err := filepath.Glob(filepath.Join(pullDir, "*.tar.zst"))
	require.NoError(t, err)
	require.Len(t, pulled, 1, "expected exactly one package pulled back")

	wantBytes, err := os.ReadFile(pkgPath)
	require.NoError(t, err)
	gotBytes, err := os.ReadFile(pulled[0])
	require.NoError(t, err)
	require.Equal(t, wantBytes, gotBytes, "pulled package must be byte-identical to the published one")
}
