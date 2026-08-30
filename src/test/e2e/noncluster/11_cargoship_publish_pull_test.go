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

package noncluster

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/src/test"
	"github.com/stretchr/testify/require"
)

// Publish always tags the package as "<repository>/<metadata.name>:<metadata.version>"
// regardless of the reference passed on the CLI, so these are the coordinates the testdata
// package lands under.
const (
	minimalPackageName    = "e2e-minimal"
	minimalPackageVersion = "0.0.1"
)

// TestCargoshipPublishPullRoundTrip exercises the built cargoship binary's "publish" and
// "pull" commands end-to-end against an in-memory OCI registry, avoiding any dependency on
// a real registry or network access for the OCI transport itself.
func TestCargoshipPublishPullRoundTrip(t *testing.T) {
	pkgPath := minimalPackage(t)
	addr := test.SetupInMemoryRegistry(t)
	dst, src := ociRefs(addr, "e2e-test")

	_, _, err := e2e.Cargoship(t, "publish", pkgPath, dst, "--plain-http", "--confirm")
	require.NoError(t, err)

	pullDir := t.TempDir()
	_, _, err = e2e.Cargoship(t, "pull", src, "--plain-http", "-o", pullDir)
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

// TestCargoshipPublish covers publish's argument validation.
func TestCargoshipPublish(t *testing.T) {
	t.Run("destination without an oci:// prefix errors", func(t *testing.T) {
		addr := test.SetupInMemoryRegistry(t)

		_, _, err := e2e.Cargoship(t, "publish", minimalPackage(t), addr+"/e2e-test", "--plain-http", "--confirm")
		require.Error(t, err)
	})

	t.Run("missing source package errors", func(t *testing.T) {
		addr := test.SetupInMemoryRegistry(t)
		dst, _ := ociRefs(addr, "e2e-test")

		_, _, err := e2e.Cargoship(t, "publish", filepath.Join(t.TempDir(), "nope.tar.zst"), dst, "--plain-http", "--confirm")
		require.Error(t, err)
	})

	t.Run("one arg errors", func(t *testing.T) {
		_, _, err := e2e.Cargoship(t, "publish", minimalPackage(t))
		require.Error(t, err)
	})
}

// TestCargoshipPublishVerify covers publish's verification flags. publish registers the
// full verify flag set, but until the loader forwarded VerificationStrategy for local
// package sources these flags were accepted and silently ignored, so an unverifiable
// package published anyway.
func TestCargoshipPublishVerify(t *testing.T) {
	t.Run("verify always on an unsigned package errors before anything is published", func(t *testing.T) {
		addr := test.SetupInMemoryRegistry(t)
		dst, src := ociRefs(addr, "e2e-test")

		_, stderr, err := e2e.Cargoship(t, "publish", minimalPackage(t), dst,
			"--plain-http", "--confirm", "--verify=always")
		require.Error(t, err)
		require.Contains(t, stderr, "no verification material available")

		// The failure must happen at load, before the package reaches the registry.
		_, _, err = e2e.Cargoship(t, "pull", src, "--plain-http", "-o", t.TempDir())
		require.Error(t, err, "nothing should have been pushed")
	})

	t.Run("unsigned package publishes when verification is not enforced", func(t *testing.T) {
		addr := test.SetupInMemoryRegistry(t)
		dst, _ := ociRefs(addr, "e2e-test")

		_, _, err := e2e.Cargoship(t, "publish", minimalPackage(t), dst, "--plain-http", "--confirm")
		require.NoError(t, err)
	})

	t.Run("verify always with the matching key publishes", func(t *testing.T) {
		pkgPath, pubPath := signedPackage(t)
		addr := test.SetupInMemoryRegistry(t)
		dst, src := ociRefs(addr, "e2e-test")

		_, _, err := e2e.Cargoship(t, "publish", pkgPath, dst,
			"--plain-http", "--confirm", "--verify=always", "-k", pubPath)
		require.NoError(t, err)

		pullDir := t.TempDir()
		_, _, err = e2e.Cargoship(t, "pull", src, "--plain-http", "-o", pullDir, "--verify=always", "-k", pubPath)
		require.NoError(t, err)
		requireSinglePackage(t, pullDir)
	})

	t.Run("verify always with an unrelated key errors", func(t *testing.T) {
		pkgPath, _ := signedPackage(t)
		_, otherPubPath := cosignKeyPair(t)
		addr := test.SetupInMemoryRegistry(t)
		dst, _ := ociRefs(addr, "e2e-test")

		_, _, err := e2e.Cargoship(t, "publish", pkgPath, dst,
			"--plain-http", "--confirm", "--verify=always", "-k", otherPubPath)
		require.Error(t, err)
	})

	t.Run("verify never skips a signature that would not validate", func(t *testing.T) {
		pkgPath, _ := signedPackage(t)
		_, otherPubPath := cosignKeyPair(t)
		addr := test.SetupInMemoryRegistry(t)
		dst, _ := ociRefs(addr, "e2e-test")

		_, _, err := e2e.Cargoship(t, "publish", pkgPath, dst,
			"--plain-http", "--confirm", "--verify=never", "-k", otherPubPath)
		require.NoError(t, err)
	})
}

// TestCargoshipVerifyFlagParsing pins how --verify takes its value. It carried a
// NoOptDefVal, which made the space-separated form parse the mode as a positional
// argument and fail with an argument-count error.
func TestCargoshipVerifyFlagParsing(t *testing.T) {
	t.Run("space-separated value is accepted", func(t *testing.T) {
		pkgPath, pubPath := signedPackage(t)
		addr := test.SetupInMemoryRegistry(t)
		dst, _ := ociRefs(addr, "e2e-test")

		_, _, err := e2e.Cargoship(t, "publish", pkgPath, dst,
			"--plain-http", "--confirm", "--verify", "always", "-k", pubPath)
		require.NoError(t, err)
	})

	// Run directly rather than through e2e.Cargoship: that helper appends --no-color and
	// --tmpdir, and a trailing --verify would consume the first of them as its value.
	t.Run("without a value errors", func(t *testing.T) {
		cmd := exec.CommandContext(t.Context(), e2e.CargoBinPath,
			"pull", "oci://example.invalid/nope:0.0.1", "-o", t.TempDir(), "--no-color", "--verify")
		out, err := cmd.CombinedOutput()
		require.Error(t, err)
		require.Contains(t, string(out), "flag needs an argument")
	})

	t.Run("an unknown mode errors", func(t *testing.T) {
		_, stderr, err := e2e.Cargoship(t, "pull", "oci://example.invalid/nope:0.0.1", "-o", t.TempDir(), "--verify", "sometimes")
		require.Error(t, err)
		require.Contains(t, stderr, "must be never, if-possible, or always")
	})
}

// TestCargoshipPull covers pull's shasum check and its error paths.
func TestCargoshipPull(t *testing.T) {
	pkgPath := minimalPackage(t)
	addr := test.SetupInMemoryRegistry(t)
	dst, src := ociRefs(addr, "e2e-test")

	_, _, err := e2e.Cargoship(t, "publish", pkgPath, dst, "--plain-http", "--confirm")
	require.NoError(t, err)

	// For OCI sources --shasum is the manifest digest, which the CLI appends to the
	// reference as "<ref>@sha256:<shasum>".
	t.Run("matching manifest digest succeeds", func(t *testing.T) {
		pullDir := t.TempDir()
		_, _, err := e2e.Cargoship(t, "pull", src, "--plain-http", "-o", pullDir,
			"--shasum", manifestDigest(t, addr, "e2e-test/"+minimalPackageName, minimalPackageVersion))
		require.NoError(t, err)
		requireSinglePackage(t, pullDir)
	})

	t.Run("mismatched manifest digest errors", func(t *testing.T) {
		wrong := sha256.Sum256([]byte("not the manifest"))

		_, _, err := e2e.Cargoship(t, "pull", src, "--plain-http", "-o", t.TempDir(),
			"--shasum", hex.EncodeToString(wrong[:]))
		require.Error(t, err)
	})

	t.Run("unknown tag errors", func(t *testing.T) {
		missing := fmt.Sprintf("oci://%s/e2e-test/%s:9.9.9", addr, minimalPackageName)

		_, _, err := e2e.Cargoship(t, "pull", missing, "--plain-http", "-o", t.TempDir())
		require.Error(t, err)
	})

	t.Run("no args errors", func(t *testing.T) {
		_, _, err := e2e.Cargoship(t, "pull")
		require.Error(t, err)
	})
}

// TestCargoshipPullHTTP covers pull's http(s) source branch, where --shasum is the SHA256
// of the package file itself and is mandatory.
func TestCargoshipPullHTTP(t *testing.T) {
	pkgPath := minimalPackage(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, pkgPath)
	}))
	t.Cleanup(srv.Close)
	srcURL := srv.URL + "/" + filepath.Base(pkgPath)

	t.Run("matching shasum succeeds", func(t *testing.T) {
		pullDir := t.TempDir()
		_, _, err := e2e.Cargoship(t, "pull", srcURL, "-o", pullDir, "--shasum", sha256File(t, pkgPath))
		require.NoError(t, err)
		requireSinglePackage(t, pullDir)
	})

	t.Run("mismatched shasum errors", func(t *testing.T) {
		wrong := sha256.Sum256([]byte("not the package"))

		_, _, err := e2e.Cargoship(t, "pull", srcURL, "-o", t.TempDir(), "--shasum", hex.EncodeToString(wrong[:]))
		require.Error(t, err)
	})

	t.Run("missing shasum errors", func(t *testing.T) {
		_, _, err := e2e.Cargoship(t, "pull", srcURL, "-o", t.TempDir())
		require.Error(t, err)
	})
}

// manifestDigest asks the registry for the manifest digest behind repo:tag and returns it
// without the "sha256:" prefix, which is the form --shasum expects.
func manifestDigest(t *testing.T, addr string, repo string, tag string) string {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodHead,
		fmt.Sprintf("http://%s/v2/%s/manifests/%s", addr, repo, tag), nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)

	digest := resp.Header.Get("Docker-Content-Digest")
	require.NotEmpty(t, digest, "registry did not report a manifest digest")

	return strings.TrimPrefix(digest, "sha256:")
}

// ociRefs returns the publish destination and the resulting pull source for the testdata
// package in the given repository namespace.
func ociRefs(addr string, namespace string) (dst string, src string) {
	dst = fmt.Sprintf("oci://%s/%s", addr, strings.Trim(namespace, "/"))
	src = fmt.Sprintf("%s/%s:%s", dst, minimalPackageName, minimalPackageVersion)

	return dst, src
}
