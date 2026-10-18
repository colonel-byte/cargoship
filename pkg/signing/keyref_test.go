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
	"path/filepath"
	"strings"
	"testing"
)

// TestSignAndVerifyThroughAnEnvKeyRef is the regression test for `env://`. It survived the
// keyless and KMS removal because it needs no cloud, it is what a CI pipeline uses to keep a
// private key off the build host, and it is the form the release pipeline documents - but
// every other test in this package hands over a file path, so nothing else would notice it
// breaking.
func TestSignAndVerifyThroughAnEnvKeyRef(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	const password = "env-ref-password"

	keyPath, pubPath := writeKeyPair(t, dir, password)
	privPEM, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("failed to read the generated private key: %v", err)
	}
	pubPEM, err := os.ReadFile(pubPath)
	if err != nil {
		t.Fatalf("failed to read the generated public key: %v", err)
	}
	t.Setenv("CARGOSHIP_TEST_PRIVATE_KEY", string(privPEM))
	t.Setenv("CARGOSHIP_TEST_PUBLIC_KEY", string(pubPEM))

	blobPath := filepath.Join(dir, "distro.yaml")
	if err := os.WriteFile(blobPath, []byte("apiVersion: colonel-byte.dev/v1alpha1\n"), 0o600); err != nil {
		t.Fatalf("failed to write the blob: %v", err)
	}
	bundlePath := filepath.Join(dir, "distro.bundle.sig")

	signOpts := DefaultSignBlobOptions()
	signOpts.Key = "env://CARGOSHIP_TEST_PRIVATE_KEY"
	signOpts.Password = password
	signOpts.BundlePath = bundlePath
	if _, err := CosignSignBlobWithOptions(ctx, blobPath, signOpts); err != nil {
		t.Fatalf("signing through env:// failed: %v", err)
	}

	verifyOpts := DefaultVerifyBlobOptions()
	verifyOpts.Key = "env://CARGOSHIP_TEST_PUBLIC_KEY"
	verifyOpts.BundlePath = bundlePath
	if err := CosignVerifyBlobWithOptions(ctx, blobPath, verifyOpts); err != nil {
		t.Fatalf("verifying through env:// failed: %v", err)
	}

	// A file path still works, against a bundle an env:// reference produced.
	verifyOpts.Key = pubPath
	if err := CosignVerifyBlobWithOptions(ctx, blobPath, verifyOpts); err != nil {
		t.Fatalf("verifying with a key file failed: %v", err)
	}
}

func TestReadKeyMaterialRejections(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "cosign.pub")
	if err := os.WriteFile(keyFile, []byte("pem-bytes"), 0o600); err != nil {
		t.Fatalf("failed to write the key file: %v", err)
	}

	for _, tt := range []struct {
		name    string
		ref     string
		env     map[string]string
		want    string
		wantPEM string
	}{
		{
			name:    "a file path is read from disk",
			ref:     keyFile,
			wantPEM: "pem-bytes",
		},
		{
			name:    "an env reference reads the variable",
			ref:     "env://CARGOSHIP_TEST_KEYREF",
			env:     map[string]string{"CARGOSHIP_TEST_KEYREF": "pem-from-env"},
			wantPEM: "pem-from-env",
		},
		{
			name: "an unset variable is named",
			ref:  "env://CARGOSHIP_TEST_ABSENT",
			want: "is not set",
		},
		{
			name: "an empty variable is named",
			ref:  "env://CARGOSHIP_TEST_EMPTY",
			env:  map[string]string{"CARGOSHIP_TEST_EMPTY": ""},
			want: "is empty",
		},
		{
			name: "env with no variable name",
			ref:  "env://",
			want: "names no environment variable",
		},
		{
			name: "an empty reference",
			ref:  "",
			want: "no key reference given",
		},
		// The KMS schemes have to fail by name. Falling through to os.ReadFile would
		// report a missing file for something nobody meant as a path.
		{
			name: "aws kms is rejected by name",
			ref:  "awskms:///alias/cargoship",
			want: "awskms key references are no longer supported",
		},
		{
			name: "hashicorp vault is rejected by name",
			ref:  "hashivault://cargoship",
			want: "hashivault key references are no longer supported",
		},
		{
			name: "openbao is rejected by name",
			ref:  "openbao://cargoship",
			want: "openbao key references are no longer supported",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			got, err := readKeyMaterial(tt.ref)
			if tt.want != "" {
				if err == nil {
					t.Fatalf("readKeyMaterial(%q) returned no error, want one naming %q", tt.ref, tt.want)
				}
				if !strings.Contains(err.Error(), tt.want) {
					t.Errorf("readKeyMaterial(%q) error = %q, want it to mention %q", tt.ref, err, tt.want)
				}
				return
			}
			if err != nil {
				t.Fatalf("readKeyMaterial(%q) unexpected error: %v", tt.ref, err)
			}
			if string(got) != tt.wantPEM {
				t.Errorf("readKeyMaterial(%q) = %q, want %q", tt.ref, got, tt.wantPEM)
			}
		})
	}
}
