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
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"golang.org/x/crypto/ssh"
)

// sshKey is one generated SSH key pair in the two encodings the code under test consumes: the PEM
// private key an identity file holds, and the authorized_keys line a recipients file holds.
type sshKey struct {
	private    []byte
	authorized string
}

// newEd25519SSHKey generates an ssh-ed25519 key pair in process, so the tests need no ssh-keygen
// binary and no key material committed to the repository.
func newEd25519SSHKey(t *testing.T) sshKey {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey() error = %v", err)
	}
	return marshalSSHKey(t, priv, pub, "")
}

// newRSASSHKey generates an ssh-rsa key pair. 2048 bits is the smallest size OpenSSH still accepts,
// and generating a larger one only makes the test slower.
func newRSASSHKey(t *testing.T) sshKey {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey() error = %v", err)
	}
	return marshalSSHKey(t, priv, priv.Public(), "")
}

// newEncryptedEd25519SSHKey generates an ssh-ed25519 key pair whose private key is protected by a
// passphrase.
func newEncryptedEd25519SSHKey(t *testing.T, passphrase string) sshKey {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey() error = %v", err)
	}
	return marshalSSHKey(t, priv, pub, passphrase)
}

func marshalSSHKey(t *testing.T, priv, pub any, passphrase string) sshKey {
	t.Helper()

	var (
		block *pem.Block
		err   error
	)
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(priv, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	}
	if err != nil {
		t.Fatalf("marshalling the private key: %v", err)
	}

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("ssh.NewPublicKey() error = %v", err)
	}

	return sshKey{
		private:    pem.EncodeToMemory(block),
		authorized: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))),
	}
}

// writeKey writes a private key to a file in a temporary directory and returns its path.
func writeKey(t *testing.T, name string, key sshKey) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, key.private, 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

// roundTrip encrypts a value to recipients and decrypts it with identities, which is the only
// property of an SSH key that matters here: that it works as both halves of the age pair.
func roundTrip(t *testing.T, recipients []age.Recipient, identities []age.Identity) {
	t.Helper()

	const value = "a registry password"

	ciphertext, err := encryptAge(value, recipients)
	if err != nil {
		t.Fatalf("encryptAge() error = %v", err)
	}
	got, err := decryptAge(ciphertext, identities)
	if err != nil {
		t.Fatalf("decryptAge() error = %v", err)
	}
	if got != value {
		t.Errorf("decryptAge() = %q, want %q", got, value)
	}
}

func TestSSHEd25519KeyRoundTrips(t *testing.T) {
	key := newEd25519SSHKey(t)
	path := writeKey(t, "id_ed25519", key)

	recipient, err := parseRecipient(key.authorized)
	if err != nil {
		t.Fatalf("parseRecipient() error = %v", err)
	}
	identities, err := parseIdentityFile(path, key.private, nil)
	if err != nil {
		t.Fatalf("parseIdentityFile() error = %v", err)
	}

	roundTrip(t, []age.Recipient{recipient}, identities)
}

func TestSSHRSAKeyRoundTrips(t *testing.T) {
	key := newRSASSHKey(t)
	path := writeKey(t, "id_rsa", key)

	recipient, err := parseRecipient(key.authorized)
	if err != nil {
		t.Fatalf("parseRecipient() error = %v", err)
	}
	identities, err := parseIdentityFile(path, key.private, nil)
	if err != nil {
		t.Fatalf("parseIdentityFile() error = %v", err)
	}

	roundTrip(t, []age.Recipient{recipient}, identities)
}

// TestParseRecipientsMixesNativeAndSSHKeys covers the reason this package parses recipients itself
// rather than calling age.ParseRecipients: that function fails the whole file on the SSH line.
func TestParseRecipientsMixesNativeAndSSHKeys(t *testing.T) {
	native, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("GenerateX25519Identity() error = %v", err)
	}
	ssh := newEd25519SSHKey(t)

	file := "# the team\n" +
		native.Recipient().String() + "\n" +
		"\n" +
		ssh.authorized + "\n"

	recipients, err := parseRecipients(strings.NewReader(file))
	if err != nil {
		t.Fatalf("parseRecipients() error = %v", err)
	}
	if len(recipients) != 2 {
		t.Fatalf("parseRecipients() returned %d recipients, want 2", len(recipients))
	}

	// Either key alone has to read a value encrypted to both, which is what makes a mixed
	// recipients file usable during a migration rather than only at the end of one.
	sshIdentities, err := parseIdentityFile("id_ed25519", ssh.private, nil)
	if err != nil {
		t.Fatalf("parseIdentityFile() error = %v", err)
	}
	roundTrip(t, recipients, sshIdentities)
	roundTrip(t, recipients, []age.Identity{native})
}

// TestParseRecipientsReadsAnAuthorizedKeysLine covers the form these keys are actually distributed
// in: with login options in front and a comment behind.
func TestParseRecipientsReadsAnAuthorizedKeysLine(t *testing.T) {
	key := newEd25519SSHKey(t)
	line := `no-agent-forwarding,command="/bin/true" ` + key.authorized + " operator@example.com"

	recipients, err := parseRecipients(strings.NewReader(line + "\n"))
	if err != nil {
		t.Fatalf("parseRecipients() error = %v", err)
	}
	if len(recipients) != 1 {
		t.Fatalf("parseRecipients() returned %d recipients, want 1", len(recipients))
	}

	identities, err := parseIdentityFile("id_ed25519", key.private, nil)
	if err != nil {
		t.Fatalf("parseIdentityFile() error = %v", err)
	}
	roundTrip(t, recipients, identities)
}

// TestParseRecipientsFailsTheFileOnAnUnusableKey holds the decision that an unsupported key type is
// an error rather than a skipped line: skipping would encrypt to fewer recipients than the operator
// listed, and the person who finds out is the one who cannot decrypt.
func TestParseRecipientsFailsTheFileOnAnUnusableKey(t *testing.T) {
	key := newEd25519SSHKey(t)
	file := key.authorized + "\n" +
		"ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBHkYc1Zd0kN2vE7+8TrFWs3PFNDPmYLWbXEtGWd8KgH1Jn9L1fJgWDxBGqaoLrKNBdRHmKzxRGxKxdmFmXlGqBM= operator@example.com\n"

	_, err := parseRecipients(strings.NewReader(file))
	if err == nil {
		t.Fatal("parseRecipients() error = nil, want one naming the bad line")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("parseRecipients() error = %q, want it to name line 2", err)
	}
}

// TestParseRecipientDoesNotEchoPrivateKeys guards the one thing a parse error must never do. age's
// own parser omits the argument for exactly this reason; agessh's does not, so this package checks
// before handing anything to it.
func TestParseRecipientDoesNotEchoPrivateKeys(t *testing.T) {
	native, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("GenerateX25519Identity() error = %v", err)
	}
	ssh := newEd25519SSHKey(t)

	tests := []struct {
		name   string
		secret string
	}{
		{"an age identity", native.String()},
		{"an SSH private key", strings.SplitN(string(ssh.private), "\n", 2)[0]},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseRecipient(tt.secret)
			if err == nil {
				t.Fatal("parseRecipient() error = nil, want one refusing private key material")
			}
			if strings.Contains(err.Error(), tt.secret) {
				t.Errorf("parseRecipient() error = %q, want it not to quote the key back", err)
			}
		})
	}
}

// TestEncryptedSSHIdentityAsksForItsPassphrase covers both halves of the deferred ask: that the
// callback is not invoked while the identity is merely being loaded, and that it is invoked once a
// stanza matches.
func TestEncryptedSSHIdentityAsksForItsPassphrase(t *testing.T) {
	const passphrase = "hunter2"

	key := newEncryptedEd25519SSHKey(t, passphrase)
	path := writeKey(t, "id_ed25519", key)

	var asked int
	identities, err := parseIdentityFile(path, key.private, func(p string) ([]byte, error) {
		asked++
		if p != path {
			t.Errorf("passphrase callback got path %q, want %q", p, path)
		}
		return []byte(passphrase), nil
	})
	if err != nil {
		t.Fatalf("parseIdentityFile() error = %v", err)
	}
	if asked != 0 {
		t.Errorf("the passphrase was asked for %d times while loading the key, want 0", asked)
	}

	recipient, err := parseRecipient(key.authorized)
	if err != nil {
		t.Fatalf("parseRecipient() error = %v", err)
	}
	roundTrip(t, []age.Recipient{recipient}, identities)

	if asked != 1 {
		t.Errorf("the passphrase was asked for %d times, want 1", asked)
	}
}

// TestEncryptedSSHIdentityWithoutAWayToAsk is the CI case: nothing can answer a prompt, so the key
// has to fail rather than block on a read that will never return.
func TestEncryptedSSHIdentityWithoutAWayToAsk(t *testing.T) {
	key := newEncryptedEd25519SSHKey(t, "hunter2")
	path := writeKey(t, "id_ed25519", key)

	_, err := parseIdentityFile(path, key.private, nil)
	if err == nil {
		t.Fatal("parseIdentityFile() error = nil, want one refusing the encrypted key")
	}
	if !strings.Contains(err.Error(), "passphrase") {
		t.Errorf("parseIdentityFile() error = %q, want it to mention the passphrase", err)
	}
}

// TestEncryptedSSHIdentityUsesThePubFileBeside covers the older PEM formats, which carry no public
// key beside the encrypted private one. The path is derived from the one the operator named, so
// reading it is not the implicit discovery the age support otherwise refuses.
func TestEncryptedSSHIdentityUsesThePubFileBeside(t *testing.T) {
	const passphrase = "hunter2"

	key := newEncryptedEd25519SSHKey(t, passphrase)
	path := writeKey(t, "id_ed25519", key)
	if err := os.WriteFile(path+".pub", []byte(key.authorized+"\n"), 0o644); err != nil { //nolint:gosec // a public key is not secret
		t.Fatalf("writing the public key: %v", err)
	}

	// Passing a nil public key is what a PEM key's PassphraseMissingError carries, so this reaches
	// encryptedSSHIdentity the same way that key would.
	identities, err := encryptedSSHIdentity(path, key.private, nil, func(string) ([]byte, error) {
		return []byte(passphrase), nil
	})
	if err != nil {
		t.Fatalf("encryptedSSHIdentity() error = %v", err)
	}

	recipient, err := parseRecipient(key.authorized)
	if err != nil {
		t.Fatalf("parseRecipient() error = %v", err)
	}
	roundTrip(t, []age.Recipient{recipient}, identities)
}

func TestEncryptedSSHIdentityNamesTheMissingPubFile(t *testing.T) {
	key := newEncryptedEd25519SSHKey(t, "hunter2")
	path := writeKey(t, "id_ed25519", key)

	_, err := encryptedSSHIdentity(path, key.private, nil, func(string) ([]byte, error) {
		return nil, nil
	})
	if err == nil {
		t.Fatal("encryptedSSHIdentity() error = nil, want one naming the missing .pub file")
	}
	if !strings.Contains(err.Error(), path+".pub") {
		t.Errorf("encryptedSSHIdentity() error = %q, want it to name %s", err, path+".pub")
	}
}

// TestAgeRecipientsInRefusesAnSSHKey holds `vault keygen --public-key` to answering rather than
// guessing: agessh recipients have no text encoding to print, and ssh-keygen already wrote one.
func TestAgeRecipientsInRefusesAnSSHKey(t *testing.T) {
	key := newEd25519SSHKey(t)

	_, err := AgeRecipientsIn(strings.NewReader(string(key.private)))
	if err == nil {
		t.Fatal("AgeRecipientsIn() error = nil, want one refusing the SSH key")
	}
	if !strings.Contains(err.Error(), ".pub") {
		t.Errorf("AgeRecipientsIn() error = %q, want it to point at the .pub file", err)
	}
}
