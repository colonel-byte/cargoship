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
	"io"
	"strings"
	"time"

	"filippo.io/age"
	"filippo.io/age/armor"
)

// encryptAge encrypts value to every recipient, returning armored age ciphertext.
//
// The output is armored rather than raw, because these values are stored as YAML scalars and a
// YAML scalar holds text. Armor is age's own answer to that -- PEM framing over base64 -- so
// using it keeps what cargoship writes readable by the age CLI, which is the point of supporting
// the format at all.
func encryptAge(value string, recipients []age.Recipient) (string, error) {
	if len(recipients) == 0 {
		return "", ErrNoAgeRecipients
	}

	var buf bytes.Buffer
	if err := writeAge(&buf, value, recipients); err != nil {
		return "", fmt.Errorf("encrypting value to age recipients: %w", err)
	}
	return buf.String(), nil
}

// writeAge performs the sequence age's streaming API needs, kept apart from encryptAge so that
// every way it can fail is wrapped in one message rather than four.
func writeAge(dst io.Writer, value string, recipients []age.Recipient) error {
	armorWriter := armor.NewWriter(dst)

	w, err := age.Encrypt(armorWriter, recipients...)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, value); err != nil {
		return err
	}

	// The age writer is closed before the armor writer, and both have to be: the first encrypts
	// and flushes the final chunk, the second writes the footer that terminates the block. Closing
	// only the outer one would produce a truncated value that still looks well-formed.
	if err := w.Close(); err != nil {
		return err
	}
	return armorWriter.Close()
}

// decryptAge decrypts armored age ciphertext with whichever of identities matches it.
//
// age tries every identity, so an operator holding several keys does not have to say which one a
// given value was encrypted to -- which is just as well, because the ciphertext does not record it.
func decryptAge(value string, identities []age.Identity) (string, error) {
	if len(identities) == 0 {
		return "", ErrNoAgeIdentities
	}

	r, err := age.Decrypt(armor.NewReader(strings.NewReader(value)), identities...)
	if err != nil {
		return "", explainNoIdentityMatch(err)
	}

	plain, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// explainNoIdentityMatch rewrites age's no-identity-matched error into one that says as much about
// the value as the format allows, and leaves any other error alone.
//
// That is less than an operator would like. An age header names the *types* of recipient a value
// was encrypted to but never which keys, deliberately, so that ciphertext does not reveal who can
// read it. "None of yours fit, and here is the kind it wants" is therefore the most specific thing
// that can be said -- cargoship cannot report that a value belongs to a colleague's key, here or
// anywhere else.
func explainNoIdentityMatch(err error) error {
	var noMatch *age.NoIdentityMatchError
	if !errors.As(err, &noMatch) {
		return err
	}
	if len(noMatch.StanzaTypes) == 0 {
		return errors.New("none of the configured age identities can decrypt this value")
	}
	return fmt.Errorf("none of the configured age identities can decrypt this value; it is encrypted to %s recipients", strings.Join(noMatch.StanzaTypes, ", "))
}

// GenerateAgeIdentity returns a new X25519 key pair.
//
// X25519 rather than a choice of algorithms because it is the one age generates: the post-quantum
// recipient type cargoship accepts on the encryption side comes from somewhere else, and there is
// nothing to pick between here.
func GenerateAgeIdentity() (*age.X25519Identity, error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, fmt.Errorf("generating an age key pair: %w", err)
	}
	return id, nil
}

// WriteAgeIdentity writes id to w in the format age-keygen writes, so that the file is readable by
// the age distribution as well as by cargoship.
//
// Matching that format exactly is the point rather than a nicety. An operator who later wants to
// read a value with 'age --decrypt', or to hand the key to something else that speaks age, would be
// stranded by a file only cargoship understands, and the whole reason for supporting age is that it
// is not a format of cargoship's own.
//
// The public key is written as a comment beside the private one because it is the only copy an
// operator has once the terminal output is gone, and 'keygen -y' reads it back from here.
func WriteAgeIdentity(w io.Writer, id *age.X25519Identity) error {
	_, err := fmt.Fprintf(w, "# created: %s\n# public key: %s\n%s\n",
		time.Now().Format(time.RFC3339), id.Recipient(), id)
	if err != nil {
		return fmt.Errorf("writing the age identity: %w", err)
	}
	return nil
}

// AgeRecipientsIn reads an identity file and returns the public keys of the identities it holds.
//
// An identity that is not an X25519 key is an error rather than a line quietly passed over. The
// answer this is asked for is "who can read the values encrypted to this key", and a short answer
// to that question is worse than no answer: the operator acts on it.
func AgeRecipientsIn(r io.Reader) ([]string, error) {
	identities, err := age.ParseIdentities(r)
	if err != nil {
		return nil, fmt.Errorf("reading age identities: %w", err)
	}

	recipients := make([]string, 0, len(identities))
	for _, identity := range identities {
		x25519, ok := identity.(*age.X25519Identity)
		if !ok {
			return nil, fmt.Errorf("age identity %d of %d is a %T, which has no public key to print",
				len(recipients)+1, len(identities), identity)
		}
		recipients = append(recipients, x25519.Recipient().String())
	}
	return recipients, nil
}
