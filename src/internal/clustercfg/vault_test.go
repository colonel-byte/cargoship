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

package clustercfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	vault "github.com/sosedoff/ansible-vault-go"
)

func TestResolveVaultPasswordFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault-pass")
	if err := os.WriteFile(path, []byte("filepass\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	got, err := ResolveVaultPassword(path)
	if err != nil {
		t.Fatalf("ResolveVaultPassword() error = %v", err)
	}
	if got != "filepass" {
		t.Errorf("ResolveVaultPassword() = %q, want %q", got, "filepass")
	}
}

func TestResolveVaultPasswordFromEnvVar(t *testing.T) {
	t.Setenv(VaultPasswordEnvVar, "envpass")

	got, err := ResolveVaultPassword("")
	if err != nil {
		t.Fatalf("ResolveVaultPassword() error = %v", err)
	}
	if got != "envpass" {
		t.Errorf("ResolveVaultPassword() = %q, want %q", got, "envpass")
	}
}

func TestResolveVaultPasswordFromAnsibleEnvVar(t *testing.T) {
	t.Setenv(AnsibleVaultPasswordEnvVar, "ansiblepass")

	got, err := ResolveVaultPassword("")
	if err != nil {
		t.Fatalf("ResolveVaultPassword() error = %v", err)
	}
	if got != "ansiblepass" {
		t.Errorf("ResolveVaultPassword() = %q, want %q", got, "ansiblepass")
	}
}

func TestResolveVaultPasswordCargoshipEnvVarTakesPrecedenceOverAnsible(t *testing.T) {
	t.Setenv(VaultPasswordEnvVar, "cargoshippass")
	t.Setenv(AnsibleVaultPasswordEnvVar, "ansiblepass")

	got, err := ResolveVaultPassword("")
	if err != nil {
		t.Fatalf("ResolveVaultPassword() error = %v", err)
	}
	if got != "cargoshippass" {
		t.Errorf("ResolveVaultPassword() = %q, want %q", got, "cargoshippass")
	}
}

func TestResolveVaultPasswordFileTakesPrecedence(t *testing.T) {
	t.Setenv(VaultPasswordEnvVar, "envpass")
	path := filepath.Join(t.TempDir(), "vault-pass")
	if err := os.WriteFile(path, []byte("filepass"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	got, err := ResolveVaultPassword(path)
	if err != nil {
		t.Fatalf("ResolveVaultPassword() error = %v", err)
	}
	if got != "filepass" {
		t.Errorf("ResolveVaultPassword() = %q, want %q", got, "filepass")
	}
}

func TestResolveVaultPasswordNoneSet(t *testing.T) {
	got, err := ResolveVaultPassword("")
	if err != nil {
		t.Fatalf("ResolveVaultPassword() error = %v", err)
	}
	if got != "" {
		t.Errorf("ResolveVaultPassword() = %q, want empty", got)
	}
}

func TestResolveVaultPasswordUnreadableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist")

	_, err := ResolveVaultPassword(path)
	if err == nil {
		t.Fatal("ResolveVaultPassword() error = nil, want an error for missing file")
	}
}

func TestDecryptRegistryAuthNoEncryptedValues(t *testing.T) {
	dis := &cluster.ZarfCluster{}
	dis.Spec.Config.Registries = []cluster.ZarfClusterRegistries{
		{
			Name: "docker.io",
			Authentication: cluster.ZarfClusterRegistryAuth{
				Username: "plainuser",
				Password: "plainpass",
			},
		},
	}

	if err := DecryptRegistryAuth(dis, ""); err != nil {
		t.Fatalf("DecryptRegistryAuth() error = %v", err)
	}

	auth := dis.Spec.Config.Registries[0].Authentication
	if auth.Username != "plainuser" || auth.Password != "plainpass" {
		t.Errorf("DecryptRegistryAuth() modified plaintext fields: %+v", auth)
	}
}

func TestDecryptRegistryAuthDecryptsEncryptedFields(t *testing.T) {
	const password = "testpass"
	encryptedPass, err := vault.Encrypt("secretpass", password)
	if err != nil {
		t.Fatalf("vault.Encrypt() error = %v", err)
	}
	encryptedToken, err := vault.Encrypt("secrettoken", password)
	if err != nil {
		t.Fatalf("vault.Encrypt() error = %v", err)
	}

	dis := &cluster.ZarfCluster{}
	dis.Spec.Config.Registries = []cluster.ZarfClusterRegistries{
		{
			Name: "ghcr.io",
			Authentication: cluster.ZarfClusterRegistryAuth{
				Username: "plainuser",
				Password: encryptedPass,
				Token:    encryptedToken,
			},
		},
	}

	if err := DecryptRegistryAuth(dis, password); err != nil {
		t.Fatalf("DecryptRegistryAuth() error = %v", err)
	}

	auth := dis.Spec.Config.Registries[0].Authentication
	if auth.Username != "plainuser" {
		t.Errorf("DecryptRegistryAuth() Username = %q, want %q", auth.Username, "plainuser")
	}
	if auth.Password != "secretpass" {
		t.Errorf("DecryptRegistryAuth() Password = %q, want %q", auth.Password, "secretpass")
	}
	if auth.Token != "secrettoken" {
		t.Errorf("DecryptRegistryAuth() Token = %q, want %q", auth.Token, "secrettoken")
	}
}

func TestDecryptRegistryAuthMissingPassword(t *testing.T) {
	encrypted, err := vault.Encrypt("secretpass", "testpass")
	if err != nil {
		t.Fatalf("vault.Encrypt() error = %v", err)
	}

	dis := &cluster.ZarfCluster{}
	dis.Spec.Config.Registries = []cluster.ZarfClusterRegistries{
		{
			Name:           "ghcr.io",
			Authentication: cluster.ZarfClusterRegistryAuth{Password: encrypted},
		},
	}

	err = DecryptRegistryAuth(dis, "")
	if err == nil {
		t.Fatal("DecryptRegistryAuth() error = nil, want an error for missing vault password")
	}
}

func TestEncryptValueRoundTrip(t *testing.T) {
	encrypted, err := EncryptValue("mysecret", "testpass")
	if err != nil {
		t.Fatalf("EncryptValue() error = %v", err)
	}
	if !cluster.IsVaultEncrypted(encrypted) {
		t.Fatalf("EncryptValue() = %q, want a $ANSIBLE_VAULT-prefixed string", encrypted)
	}

	got, err := vault.Decrypt(encrypted, "testpass")
	if err != nil {
		t.Fatalf("vault.Decrypt() error = %v", err)
	}
	if got != "mysecret" {
		t.Errorf("round trip = %q, want %q", got, "mysecret")
	}
}

func TestEncryptValueEmptyPassword(t *testing.T) {
	_, err := EncryptValue("mysecret", "")
	if err == nil {
		t.Fatal("EncryptValue() error = nil, want an error for empty password")
	}
}

func TestDecryptRegistryAuthWrongPassword(t *testing.T) {
	encrypted, err := vault.Encrypt("secretpass", "testpass")
	if err != nil {
		t.Fatalf("vault.Encrypt() error = %v", err)
	}

	dis := &cluster.ZarfCluster{}
	dis.Spec.Config.Registries = []cluster.ZarfClusterRegistries{
		{
			Name:           "ghcr.io",
			Authentication: cluster.ZarfClusterRegistryAuth{Password: encrypted},
		},
	}

	err = DecryptRegistryAuth(dis, "wrongpass")
	if err == nil {
		t.Fatal("DecryptRegistryAuth() error = nil, want an error for wrong vault password")
	}
}

const testCAPEM = `-----BEGIN CERTIFICATE-----
dGhpcyBpcyBub3QgYSByZWFsIGNlcnRpZmljYXRl
-----END CERTIFICATE-----
`

// A CA certificate is public and does not need encrypting, but a document vaulted as a whole
// carries one anyway, so it is decrypted alongside the credentials.
func TestDecryptRegistryAuthDecryptsInlineCA(t *testing.T) {
	const password = "testpass"
	encryptedCA, err := vault.Encrypt(testCAPEM, password)
	if err != nil {
		t.Fatalf("vault.Encrypt() error = %v", err)
	}

	dis := &cluster.ZarfCluster{}
	dis.Spec.Config.Registries = []cluster.ZarfClusterRegistries{
		{
			Name: "ghcr.io",
			TLS:  &cluster.ZarfClusterRegistryTLS{CA: encryptedCA},
		},
	}

	if err := DecryptRegistryAuth(dis, password); err != nil {
		t.Fatalf("DecryptRegistryAuth() error = %v", err)
	}

	if got := dis.Spec.Config.Registries[0].TLS.CA; got != testCAPEM {
		t.Errorf("DecryptRegistryAuth() CA = %q, want the certificate", got)
	}
}

// Load time saw ciphertext, so the certificate is checked here instead -- the first point at
// which there is a certificate to look at.
func TestDecryptRegistryAuthRejectsDecryptedCAThatIsNotACertificate(t *testing.T) {
	const password = "testpass"
	encryptedCA, err := vault.Encrypt("not a certificate", password)
	if err != nil {
		t.Fatalf("vault.Encrypt() error = %v", err)
	}

	dis := &cluster.ZarfCluster{}
	dis.Spec.Config.Registries = []cluster.ZarfClusterRegistries{
		{
			Name: "ghcr.io",
			TLS:  &cluster.ZarfClusterRegistryTLS{CA: encryptedCA},
		},
	}

	err = DecryptRegistryAuth(dis, password)
	if err == nil {
		t.Fatal("DecryptRegistryAuth() error = nil, want an error for a decrypted value that is not a certificate")
	}
}

func TestVerifyRegistryAuthLeavesTheDocumentAlone(t *testing.T) {
	const password = "testpass"
	encryptedPass, err := vault.Encrypt("secretpass", password)
	if err != nil {
		t.Fatalf("vault.Encrypt() error = %v", err)
	}
	encryptedCA, err := vault.Encrypt(testCAPEM, password)
	if err != nil {
		t.Fatalf("vault.Encrypt() error = %v", err)
	}

	dis := &cluster.ZarfCluster{}
	dis.Spec.Config.Registries = []cluster.ZarfClusterRegistries{
		{
			Name:           "ghcr.io",
			Authentication: cluster.ZarfClusterRegistryAuth{Username: "robot", Password: encryptedPass},
			TLS:            &cluster.ZarfClusterRegistryTLS{CA: encryptedCA},
		},
	}

	if err := VerifyRegistryAuth(dis, password); err != nil {
		t.Fatalf("VerifyRegistryAuth() error = %v", err)
	}

	registry := dis.Spec.Config.Registries[0]
	if registry.Authentication.Password != encryptedPass {
		t.Errorf("VerifyRegistryAuth() decrypted Password in place: %q", registry.Authentication.Password)
	}
	if registry.TLS.CA != encryptedCA {
		t.Errorf("VerifyRegistryAuth() decrypted TLS CA in place: %q", registry.TLS.CA)
	}
}

func TestVerifyRegistryAuthMissingPassword(t *testing.T) {
	encrypted, err := vault.Encrypt("secretpass", "testpass")
	if err != nil {
		t.Fatalf("vault.Encrypt() error = %v", err)
	}

	dis := &cluster.ZarfCluster{}
	dis.Spec.Config.Registries = []cluster.ZarfClusterRegistries{
		{
			Name:           "ghcr.io",
			Authentication: cluster.ZarfClusterRegistryAuth{Username: "robot", Password: encrypted},
		},
	}

	err = VerifyRegistryAuth(dis, "")
	if err == nil {
		t.Fatal("VerifyRegistryAuth() error = nil, want an error for missing vault password")
	}
	// The error names the field so that a document vaulting several of them says which one.
	if !strings.Contains(err.Error(), "auth.pass") {
		t.Errorf("VerifyRegistryAuth() error = %v, want it to name auth.pass", err)
	}
}

func TestVerifyRegistryAuthWrongPassword(t *testing.T) {
	encrypted, err := vault.Encrypt("secretpass", "testpass")
	if err != nil {
		t.Fatalf("vault.Encrypt() error = %v", err)
	}

	dis := &cluster.ZarfCluster{}
	dis.Spec.Config.Registries = []cluster.ZarfClusterRegistries{
		{
			Name:           "ghcr.io",
			Authentication: cluster.ZarfClusterRegistryAuth{Username: "robot", Password: encrypted},
		},
	}

	if err := VerifyRegistryAuth(dis, "wrongpass"); err == nil {
		t.Fatal("VerifyRegistryAuth() error = nil, want an error for wrong vault password")
	}
}

// A vault-encrypted CA that turns out not to be a certificate is caught here too, rather than on
// the node it was about to be written to.
func TestVerifyRegistryAuthRejectsDecryptedCAThatIsNotACertificate(t *testing.T) {
	const password = "testpass"
	encryptedCA, err := vault.Encrypt("not a certificate", password)
	if err != nil {
		t.Fatalf("vault.Encrypt() error = %v", err)
	}

	dis := &cluster.ZarfCluster{}
	dis.Spec.Config.Registries = []cluster.ZarfClusterRegistries{
		{
			Name: "ghcr.io",
			TLS:  &cluster.ZarfClusterRegistryTLS{CA: encryptedCA},
		},
	}

	if err := VerifyRegistryAuth(dis, password); err == nil {
		t.Fatal("VerifyRegistryAuth() error = nil, want an error for a decrypted value that is not a certificate")
	}
}

func TestVerifyRegistryAuthNothingEncrypted(t *testing.T) {
	dis := &cluster.ZarfCluster{}
	dis.Spec.Config.Registries = []cluster.ZarfClusterRegistries{
		{
			Name:           "docker.io",
			Authentication: cluster.ZarfClusterRegistryAuth{Username: "plainuser", Password: "plainpass"},
		},
	}

	if err := VerifyRegistryAuth(dis, ""); err != nil {
		t.Fatalf("VerifyRegistryAuth() error = %v", err)
	}
}
