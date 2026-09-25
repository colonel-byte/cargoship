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

package load

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
)

const sopsClusterFixture = "apiVersion: zarf.dev/v1alpha1\nkind: ZarfCluster\nmetadata:\n  name: sops-fixture\n"

// sopsBinary skips the calling test when the sops CLI isn't on PATH, needed here only to build the
// fixture -- ClusterDefinition itself never shells out, it calls clustercfg.DecryptSops.
func sopsBinary(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("sops")
	if err != nil {
		t.Skip("sops binary not on PATH, skipping sops fixture test")
	}
	return path
}

func writeSopsFixture(t *testing.T, sopsPath, dir string, recipient *age.X25519Recipient) string {
	t.Helper()

	in := filepath.Join(dir, "cluster-in.yaml")
	if err := os.WriteFile(in, []byte(sopsClusterFixture), 0o600); err != nil {
		t.Fatalf("writing cleartext fixture: %v", err)
	}

	cmd := exec.Command(sopsPath, "--encrypt", "--age", recipient.String(), "--input-type", "yaml", "--output-type", "yaml", in)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("sops --encrypt: %v: %s", err, stderr.String())
	}

	out := filepath.Join(dir, "cluster.yaml")
	if err := os.WriteFile(out, stdout.Bytes(), 0o600); err != nil {
		t.Fatalf("writing ciphertext fixture: %v", err)
	}
	return out
}

func TestClusterDefinitionSopsEncrypted(t *testing.T) {
	sopsPath := sopsBinary(t)
	dir := t.TempDir()

	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("age.GenerateX25519Identity() error = %v", err)
	}
	configPath := writeSopsFixture(t, sopsPath, dir, id.Recipient())

	identityFile := filepath.Join(dir, "identity.txt")
	if err := os.WriteFile(identityFile, []byte(id.String()+"\n"), 0o600); err != nil {
		t.Fatalf("writing age identity file: %v", err)
	}

	t.Run("decrypts with the right key", func(t *testing.T) {
		t.Setenv("SOPS_AGE_KEY_FILE", identityFile)

		cluster, err := ClusterDefinition(context.Background(), configPath, ClusterOptions{})
		if err != nil {
			t.Fatalf("ClusterDefinition() error = %v", err)
		}
		if cluster.Metadata.Name != "sops-fixture" {
			t.Errorf("cluster.Metadata.Name = %q, want %q", cluster.Metadata.Name, "sops-fixture")
		}
	})

	t.Run("fails naming the file when no key is available", func(t *testing.T) {
		t.Setenv("SOPS_AGE_KEY_FILE", filepath.Join(dir, "does-not-exist.txt"))

		_, err := ClusterDefinition(context.Background(), configPath, ClusterOptions{})
		if err == nil {
			t.Fatal("ClusterDefinition() error = nil, want an error when no usable key is configured")
		}
		if !strings.Contains(err.Error(), configPath) {
			t.Errorf("ClusterDefinition() error = %q, want it to name the file %q", err.Error(), configPath)
		}
	})
}
