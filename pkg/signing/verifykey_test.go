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

package signing

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sigstore/cosign/v3/pkg/cosign"
)

// writeKeyPair generates a real cosign keypair in dir, the same encrypted-PEM format
// `cosign generate-key-pair` writes.
func writeKeyPair(t *testing.T, dir, password string) (keyPath, pubPath string) {
	t.Helper()

	keys, err := cosign.GenerateKeyPair(func(bool) ([]byte, error) { return []byte(password), nil })
	if err != nil {
		t.Fatalf("failed to generate a keypair: %v", err)
	}
	keyPath = filepath.Join(dir, "cosign.key")
	pubPath = filepath.Join(dir, "cosign.pub")
	if err := os.WriteFile(keyPath, keys.PrivateBytes, 0o600); err != nil {
		t.Fatalf("failed to write the private key: %v", err)
	}
	if err := os.WriteFile(pubPath, keys.PublicBytes, 0o644); err != nil {
		t.Fatalf("failed to write the public key: %v", err)
	}
	return keyPath, pubPath
}

// TestVerifyBlobWithKeyAgainstCargoshipSignature is the proof that matters: a bundle
// produced by cargoship's current signing path verifies under the sigstore-go
// implementation, with no cosign CLI involved in the verify.
func TestVerifyBlobWithKeyAgainstCargoshipSignature(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	const password = "proof-password"

	keyPath, pubPath := writeKeyPair(t, dir, password)

	blobPath := filepath.Join(dir, "distro.yaml")
	if err := os.WriteFile(blobPath, []byte("apiVersion: colonel-byte.dev/v1alpha1\n"), 0o600); err != nil {
		t.Fatalf("failed to write the blob: %v", err)
	}
	bundlePath := filepath.Join(dir, "distro.bundle.sig")

	opts := DefaultSignBlobOptions()
	opts.Key = keyPath
	opts.Password = password
	opts.BundlePath = bundlePath
	if _, err := CosignSignBlobWithOptions(ctx, blobPath, opts); err != nil {
		t.Fatalf("CosignSignBlobWithOptions() unexpected error: %v", err)
	}

	t.Run("the signature verifies against its key", func(t *testing.T) {
		if err := VerifyBlobWithKey(pubPath, bundlePath, blobPath); err != nil {
			t.Fatalf("VerifyBlobWithKey() unexpected error: %v", err)
		}
	})

	t.Run("an unrelated key is rejected", func(t *testing.T) {
		otherDir := t.TempDir()
		_, otherPub := writeKeyPair(t, otherDir, password)
		if err := VerifyBlobWithKey(otherPub, bundlePath, blobPath); err == nil {
			t.Fatal("VerifyBlobWithKey() accepted a signature made by a different key")
		}
	})

	t.Run("a tampered blob is rejected", func(t *testing.T) {
		tampered := filepath.Join(t.TempDir(), "distro.yaml")
		if err := os.WriteFile(tampered, []byte("apiVersion: evil\n"), 0o600); err != nil {
			t.Fatalf("failed to write the tampered blob: %v", err)
		}
		if err := VerifyBlobWithKey(pubPath, bundlePath, tampered); err == nil {
			t.Fatal("VerifyBlobWithKey() accepted a signature over different content")
		}
	})

	t.Run("a missing bundle errors", func(t *testing.T) {
		if err := VerifyBlobWithKey(pubPath, filepath.Join(dir, "absent.sig"), blobPath); err == nil {
			t.Fatal("VerifyBlobWithKey() returned no error for a missing bundle")
		}
	})
}

// TestVerifyBlobWithKeyAgainstCosignCLI verifies a bundle written by the real cosign
// binary, which is how this repo's own releases are signed. The fixture is generated
// out-of-band; the test skips when it is absent so the suite stays hermetic.
func TestVerifyBlobWithKeyAgainstCosignCLI(t *testing.T) {
	dir := os.Getenv("CARGOSHIP_COSIGN_FIXTURE_DIR")
	if dir == "" {
		t.Skip("CARGOSHIP_COSIGN_FIXTURE_DIR is not set")
	}
	if err := VerifyBlobWithKey(
		filepath.Join(dir, "cosign.pub"),
		filepath.Join(dir, "blob.sigstore.json"),
		filepath.Join(dir, "blob.txt"),
	); err != nil {
		t.Fatalf("VerifyBlobWithKey() rejected a cosign-CLI bundle: %v", err)
	}
}

// TestSignBlobWithKeyRoundTrip covers the sigstore-go signing path against its own
// verifier, and against cosign's, which is the interop that matters: a package signed
// by cargoship has to verify with the cosign binary an operator already trusts.
func TestSignBlobWithKeyRoundTrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	const password = "proof-password"

	keyPath, pubPath := writeKeyPair(t, dir, password)

	blobPath := filepath.Join(dir, "distro.yaml")
	if err := os.WriteFile(blobPath, []byte("apiVersion: colonel-byte.dev/v1alpha1\nkind: Distro\n"), 0o600); err != nil {
		t.Fatalf("failed to write the blob: %v", err)
	}
	bundlePath := filepath.Join(dir, "distro.bundle.sig")

	if err := SignBlobWithKey(ctx, keyPath, password, blobPath, bundlePath); err != nil {
		t.Fatalf("SignBlobWithKey() unexpected error: %v", err)
	}

	t.Run("our verifier accepts our signature", func(t *testing.T) {
		if err := VerifyBlobWithKey(pubPath, bundlePath, blobPath); err != nil {
			t.Fatalf("VerifyBlobWithKey() rejected our own bundle: %v", err)
		}
	})

	t.Run("an unrelated key is rejected", func(t *testing.T) {
		_, otherPub := writeKeyPair(t, t.TempDir(), password)
		if err := VerifyBlobWithKey(otherPub, bundlePath, blobPath); err == nil {
			t.Fatal("VerifyBlobWithKey() accepted a signature made by a different key")
		}
	})

	t.Run("the wrong password fails to sign", func(t *testing.T) {
		err := SignBlobWithKey(ctx, keyPath, "not-the-password", blobPath, filepath.Join(dir, "wrong.sig"))
		if err == nil {
			t.Fatal("SignBlobWithKey() returned no error for the wrong key password")
		}
	})

	t.Run("cosign's own verifier accepts our signature", func(t *testing.T) {
		cosignBin, err := exec.LookPath("cosign")
		if err != nil {
			t.Skip("cosign is not on PATH")
		}
		out, err := exec.CommandContext(ctx, cosignBin, "verify-blob",
			"--key", pubPath,
			"--bundle", bundlePath,
			"--new-bundle-format",
			"--insecure-ignore-tlog",
			blobPath,
		).CombinedOutput()
		if err != nil {
			t.Fatalf("cosign verify-blob rejected our bundle: %v\n%s", err, out)
		}
	})
}
