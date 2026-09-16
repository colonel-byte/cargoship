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
