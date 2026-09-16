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
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"filippo.io/age"
	"filippo.io/age/agessh"
	"golang.org/x/crypto/ssh"
)

// keyFileSizeLimit caps how much of a recipients or identity file is read, matching the limit age
// applies to both. A key file larger than this is a mistaken path rather than a key.
const keyFileSizeLimit = 1 << 24 // 16 MiB

// pemHeaderPrefix begins every PEM block, which is how an identity file holding an SSH private key
// is told from one holding native age identities without parsing it as either first.
const pemHeaderPrefix = "-----BEGIN "

// secretPrefixes begin a line that holds private key material rather than a public key. A line like
// this is an identity file offered as a recipients file by mistake.
//
// It is worth recognising because agessh.ParseRecipient, unlike age's own parser, quotes what it
// was given back in its error. Parse errors are printed and logged, and a private key that reaches
// a log aggregator has to be treated as compromised everywhere it was used.
var secretPrefixes = []string{"AGE-SECRET-KEY-", pemHeaderPrefix}

// errRecipientIsSecret reports that something offered as a public key holds private key material.
// The offending text is deliberately absent from it, and has to stay absent from anything that
// wraps it.
var errRecipientIsSecret = errors.New("that is private key material, not a public key: a recipient is the public half of a key pair")

// PassphraseFunc is asked for the passphrase protecting the SSH private key at path.
//
// It exists so that this package does no terminal I/O of its own: the commands own the terminal and
// supply the prompt, and a caller with nowhere to prompt supplies nothing. A nil PassphraseFunc is
// therefore an error rather than a prompt, which is what makes an encrypted key fail immediately in
// CI instead of hanging on a read that will never be answered.
type PassphraseFunc func(path string) ([]byte, error)

// parseRecipient parses one public key, which may be a native age recipient or an SSH public key in
// the authorized_keys form.
func parseRecipient(key string) (age.Recipient, error) {
	if key == "" {
		return nil, errors.New("an empty string is not a public key")
	}
	for _, prefix := range secretPrefixes {
		if strings.HasPrefix(key, prefix) {
			return nil, errRecipientIsSecret
		}
	}

	// age.ParseRecipients dispatches on the age1/age1pq1 prefixes itself, and is the only exported
	// way in to either parser, so a single-line reader is how one native key is parsed.
	if strings.HasPrefix(key, "age1") {
		recipients, err := age.ParseRecipients(strings.NewReader(key))
		if err != nil {
			return nil, err
		}
		return recipients[0], nil
	}

	// agessh.ParseRecipient is ssh.ParseAuthorizedKey underneath, so an authorized_keys line
	// carrying options or a trailing comment parses without any handling here.
	return agessh.ParseRecipient(key)
}

// parseRecipients reads a file of public keys, one per line, ignoring blank lines and lines that
// begin with '#'.
//
// age.ParseRecipients would do this, except that it fails the whole file on the first line it does
// not recognise, and an SSH key is such a line. Dispatching line by line is what lets one file hold
// both kinds, which is the point of accepting SSH keys in the age flags at all.
//
// A line that parses as neither kind fails the file, naming the line. Skipping it would encrypt to
// fewer recipients than the operator listed, and the person who discovers that is the one who
// cannot decrypt.
func parseRecipients(r io.Reader) ([]age.Recipient, error) {
	var recipients []age.Recipient

	limited := &io.LimitedReader{R: r, N: keyFileSizeLimit + 1}
	scanner := bufio.NewScanner(limited)
	for n := 1; scanner.Scan(); n++ {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !utf8.ValidString(line) {
			return nil, fmt.Errorf("line %d is not valid UTF-8", n)
		}
		recipient, err := parseRecipient(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		recipients = append(recipients, recipient)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if limited.N == 0 {
		return nil, fmt.Errorf("the file is longer than %d bytes", keyFileSizeLimit)
	}
	return recipients, nil
}

// parseIdentityFile parses an identity file holding either native age identities, one per line, or
// a single PEM-encoded SSH private key.
//
// An SSH key cannot be dispatched line by line the way a recipient can, because it spans many
// lines, so the file is read whole by the caller and routed here on its first bytes. path is used
// in errors and to find the ".pub" file beside an encrypted key; contents is what was read from it.
func parseIdentityFile(path string, contents []byte, passphrase PassphraseFunc) ([]age.Identity, error) {
	if !bytes.HasPrefix(bytes.TrimLeft(contents, " \t\r\n"), []byte(pemHeaderPrefix)) {
		return age.ParseIdentities(bytes.NewReader(contents))
	}

	identity, err := agessh.ParseIdentity(contents)
	if err == nil {
		return []age.Identity{identity}, nil
	}

	// agessh.ParseIdentity returns the ssh package's error unwrapped, so the one case worth
	// recovering from -- a key the operator can supply a passphrase for -- is recognisable here.
	var missing *ssh.PassphraseMissingError
	if !errors.As(err, &missing) {
		return nil, err
	}
	return encryptedSSHIdentity(path, contents, missing.PublicKey, passphrase)
}

// encryptedSSHIdentity wraps a passphrase-protected SSH private key in an identity that asks for the
// passphrase only once a stanza matches its public key.
//
// That laziness is the reason to build this rather than decrypt the key up front: an operator
// holding a key nothing was encrypted to is never asked, and one whose key does match is asked once
// rather than once per credential, because the decrypted key is cached after the first success.
func encryptedSSHIdentity(path string, contents []byte, pubKey ssh.PublicKey, passphrase PassphraseFunc) ([]age.Identity, error) {
	if passphrase == nil {
		return nil, fmt.Errorf("%s is protected by a passphrase, and there is no terminal to ask for one on", path)
	}

	if pubKey == nil {
		var err error
		pubKey, err = publicKeyBeside(path)
		if err != nil {
			return nil, err
		}
	}

	identity, err := agessh.NewEncryptedSSHIdentity(pubKey, contents, func() ([]byte, error) {
		return passphrase(path)
	})
	if err != nil {
		return nil, err
	}
	return []age.Identity{identity}, nil
}

// publicKeyBeside reads the ".pub" file next to an SSH private key.
//
// Only the OpenSSH key format carries the public key alongside the encrypted private one, so an
// older PEM key has to get it from the file ssh-keygen wrote beside it. That is not the implicit
// discovery the age support otherwise refuses: the path is derived from one the operator named on
// the command line, and nothing is read when the key needs no passphrase.
func publicKeyBeside(path string) (ssh.PublicKey, error) {
	pubPath := path + ".pub"

	contents, err := os.ReadFile(pubPath) //nolint:gosec // the path is derived from the operator's own
	if err != nil {
		return nil, fmt.Errorf("%s is protected by a passphrase and does not carry its public key, so %s is needed to decrypt with it: %w", path, pubPath, err)
	}

	pubKey, _, _, _, err := ssh.ParseAuthorizedKey(contents)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", pubPath, err)
	}
	return pubKey, nil
}

// readKeyFile reads a file of key material whole, refusing one larger than age's own limit rather
// than reading it into memory.
func readKeyFile(path string) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // the path is the operator's own, given on the command line
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = f.Close() //nolint:errcheck // the file is open for reading, so the read error is the one worth reporting
	}()

	contents, err := io.ReadAll(io.LimitReader(f, keyFileSizeLimit+1))
	if err != nil {
		return nil, err
	}
	if len(contents) > keyFileSizeLimit {
		return nil, fmt.Errorf("%s is longer than %d bytes", path, keyFileSizeLimit)
	}
	return contents, nil
}
