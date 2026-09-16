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
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"filippo.io/age"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
)

// AgeIdentityFileEnvVar is the environment variable checked for the path to an age identity file
// when no --age-identity-file flag is given. It holds one path: a list would need a separator, and
// every separator worth choosing is legal in a file name.
const AgeIdentityFileEnvVar = "CARGOSHIP_AGE_IDENTITY_FILE"

// AgeRecipientsEnvVar is the environment variable checked for age recipients when no
// --age-recipient or --age-recipients-file flag is given. It holds whitespace-separated public
// keys, which is unambiguous in a way a list of paths is not, since a public key contains no
// whitespace.
const AgeRecipientsEnvVar = "CARGOSHIP_AGE_RECIPIENTS"

// ErrNoKeyMaterial reports that nothing was configured to encrypt with: no vault password and no
// age recipients.
var ErrNoKeyMaterial = errors.New("no vault password and no age recipients were provided")

// ErrNoAgeRecipients reports that an age encryption was asked for with no recipients to encrypt to.
var ErrNoAgeRecipients = errors.New("no age recipients configured")

// ErrNoAgeIdentities reports that an age value has to be decrypted but no identity was configured.
var ErrNoAgeIdentities = errors.New("no age identities configured")

// Format names one of the two ciphertext formats cargoship reads. The values read as they should
// in an error message, which is most of what they are for.
type Format string

const (
	// FormatVault is Ansible Vault ciphertext, keyed by a shared password.
	FormatVault Format = "Ansible Vault"
	// FormatAge is armored age ciphertext, keyed by a recipient's public key.
	FormatAge Format = "age"
)

// FormatOf reports which format value is encrypted in, and false when it is plaintext.
func FormatOf(value string) (Format, bool) {
	switch {
	case cluster.IsVaultEncrypted(value):
		return FormatVault, true
	case cluster.IsAgeEncrypted(value):
		return FormatAge, true
	default:
		return "", false
	}
}

// Keyring holds the key material cargoship encrypts and decrypts registry credentials with.
//
// One keyring can carry both formats at once, because one configuration can hold both. That is
// not only a migration state: an apply reads whatever the document has, so a file that keeps a
// legacy vaulted credential beside age-encrypted ones works indefinitely.
//
// The age types are unexported so that the packages this is threaded through -- pkg/action and
// pkg/phase, which only ever pass it along -- do not take on a dependency on the age module.
type Keyring struct {
	vaultPassword string
	// vaultExplicit records that the password came from --vault-password-file rather than from
	// the environment. EncryptFormat is the only thing that cares; see the note there.
	vaultExplicit bool

	identities []age.Identity

	recipients []age.Recipient
	// ageExplicit records the same thing as vaultExplicit, for the recipients.
	ageExplicit bool

	// sshPassphrase is how an encrypted SSH identity asks for its passphrase. It is held rather
	// than called at resolve time because the ask is deferred until a stanza matches the key.
	sshPassphrase PassphraseFunc
}

// KeyOptions is the key material a command was given, before any of it has been read or parsed.
type KeyOptions struct {
	// VaultPasswordFile is the path passed to --vault-password-file, if any.
	VaultPasswordFile string
	// AgeIdentityFiles are the paths passed to --age-identity-file, which decrypt.
	AgeIdentityFiles []string
	// AgeRecipients are the public keys passed to --age-recipient, which encrypt.
	AgeRecipients []string
	// AgeRecipientFiles are the paths passed to --age-recipients-file, each holding public keys.
	AgeRecipientFiles []string
	// SSHPassphrase is asked for the passphrase of an encrypted SSH identity. Leaving it nil says
	// the caller has nowhere to ask, which makes such a key an error rather than a hang.
	SSHPassphrase PassphraseFunc
}

// NewVaultKeyring returns a keyring holding only an Ansible Vault password, for a caller that has
// the password itself rather than the flags it came from.
func NewVaultKeyring(password string) *Keyring {
	return &Keyring{vaultPassword: password, vaultExplicit: password != ""}
}

// ResolveKeyring reads every piece of key material o names and returns it as one keyring.
//
// Nothing is discovered implicitly. An age identity is read from the path given or from
// CARGOSHIP_AGE_IDENTITY_FILE and nowhere else -- not from ~/.config/sops/age/keys.txt, not from
// any other conventional location -- for the reason ResolveVaultPassword never prompts: an apply
// that succeeds on one operator's machine because of a file the config never mentions is an apply
// nobody can reason about.
func ResolveKeyring(o KeyOptions) (*Keyring, error) {
	password, err := ResolveVaultPassword(o.VaultPasswordFile)
	if err != nil {
		return nil, err
	}

	k := &Keyring{
		vaultPassword: password,
		vaultExplicit: o.VaultPasswordFile != "",
		ageExplicit:   len(o.AgeRecipients) > 0 || len(o.AgeRecipientFiles) > 0,
		sshPassphrase: o.SSHPassphrase,
	}

	if err := k.loadIdentities(o.AgeIdentityFiles); err != nil {
		return nil, err
	}
	if err := k.loadRecipients(o.AgeRecipients, o.AgeRecipientFiles); err != nil {
		return nil, err
	}
	return k, nil
}

// loadIdentities reads the private keys that decrypt, from the files given or from the environment
// when none were. A file may hold native age identities or an SSH private key; see
// parseIdentityFile.
func (k *Keyring) loadIdentities(files []string) error {
	if len(files) == 0 {
		if env := os.Getenv(AgeIdentityFileEnvVar); env != "" {
			files = []string{env}
		}
	}

	for _, path := range files {
		contents, err := readKeyFile(path)
		if err != nil {
			return fmt.Errorf("reading age identity file: %w", err)
		}
		identities, err := parseIdentityFile(path, contents, k.sshPassphrase)
		if err != nil {
			return fmt.Errorf("parsing age identity file %s: %w", path, err)
		}
		if len(identities) == 0 {
			return fmt.Errorf("age identity file %s holds no identities", path)
		}
		k.identities = append(k.identities, identities...)
	}
	return nil
}

// loadRecipients reads the public keys that encrypt, from the flags given or from the environment
// when none were. A key may be a native age recipient or an SSH public key; see parseRecipient.
//
// A file that parses to no recipients is an error rather than nothing to do. Carrying on would
// leave the keyring with a vault password and no recipients, and EncryptFormat would then quietly
// choose Ansible Vault -- writing the secret under a key the operator did not ask for, which is
// the one outcome worth refusing outright.
func (k *Keyring) loadRecipients(keys, files []string) error {
	if len(keys) == 0 && len(files) == 0 {
		keys = strings.Fields(os.Getenv(AgeRecipientsEnvVar))
	}

	for _, key := range keys {
		parsed, err := parseRecipient(key)
		if errors.Is(err, errRecipientIsSecret) {
			// Deliberately without the key: see the note on errRecipientIsSecret.
			return fmt.Errorf("parsing an age recipient: %w", err)
		}
		if err != nil {
			return fmt.Errorf("parsing age recipient %q: %w", key, err)
		}
		k.recipients = append(k.recipients, parsed)
	}

	for _, path := range files {
		contents, err := readKeyFile(path)
		if err != nil {
			return fmt.Errorf("reading age recipients file: %w", err)
		}
		parsed, err := parseRecipients(bytes.NewReader(contents))
		if err != nil {
			return fmt.Errorf("parsing age recipients file %s: %w", path, err)
		}
		if len(parsed) == 0 {
			return fmt.Errorf("age recipients file %s holds no recipients", path)
		}
		k.recipients = append(k.recipients, parsed...)
	}
	return nil
}

// Empty reports whether the keyring holds no key material at all, which is how a command tells
// "nothing was configured" from "what was configured does not fit this value".
func (k *Keyring) Empty() bool {
	return k == nil || (k.vaultPassword == "" && len(k.identities) == 0 && len(k.recipients) == 0)
}

// CanDecrypt reports whether the keyring holds what it takes to decrypt a value in format.
func (k *Keyring) CanDecrypt(format Format) bool {
	if k == nil {
		return false
	}
	switch format {
	case FormatVault:
		return k.vaultPassword != ""
	case FormatAge:
		return len(k.identities) > 0
	default:
		return false
	}
}

// RekeyTarget returns the keyring a rekey should write with, given this keyring as the one the
// file is read with, and reports whether that target is a different key from the one the file
// already carries.
//
// The rules are the ones rekey has always had, widened by one case. An explicit new vault password
// is a rotation. age recipients are a rotation too, and are how a configuration moves from Ansible
// Vault to age: read with the old password, write to the recipients, in a single pass that never
// puts the plaintext on disk. Neither given means the target is the key the file already uses,
// which re-salts every value rather than rotating it.
//
// Both given is an error. --new-vault-password-file and the age recipients each name the key to
// write, and honouring one would mean silently ignoring the other.
func (k *Keyring) RekeyTarget(newVaultPassword string) (*Keyring, bool, error) {
	switch {
	case len(k.recipients) > 0 && newVaultPassword != "":
		return nil, false, errors.New("age recipients and a new vault password were both given; pass one or the other, since each names the key to rekey onto")
	case len(k.recipients) > 0:
		// Only the recipients are carried over. A target holding the vault password as well would
		// be a keyring naming two formats, which EncryptFormat rightly refuses.
		return &Keyring{recipients: k.recipients, ageExplicit: true}, true, nil
	case newVaultPassword != "":
		return NewVaultKeyring(newVaultPassword), newVaultPassword != k.vaultPassword, nil
	default:
		return NewVaultKeyring(k.vaultPassword), false, nil
	}
}

// EncryptFormat reports the format EncryptValue will write, and an error when the keyring gives no
// way to choose one.
//
// age recipients beat a vault password that came from the environment. CARGOSHIP_VAULT_PASSWORD
// and ANSIBLE_VAULT_PASSWORD are typically set once in a shell profile on a machine already using
// vault, so an operator who passes --age-recipient there is asking for age, not for a complaint
// about a variable they set months ago.
//
// Two explicit flags is an error instead of a third precedence rule. A value is written in one
// format, and picking which one on the operator's behalf means writing a secret under a key they
// did not choose -- the one case worth stopping for rather than guessing well.
func (k *Keyring) EncryptFormat() (Format, error) {
	switch {
	case k == nil || (len(k.recipients) == 0 && k.vaultPassword == ""):
		return "", ErrNoKeyMaterial
	case len(k.recipients) > 0 && k.ageExplicit && k.vaultExplicit:
		return "", errors.New("age recipients and a vault password file were both given; pass one or the other, since a value is written in a single format")
	case len(k.recipients) > 0:
		return FormatAge, nil
	default:
		return FormatVault, nil
	}
}
