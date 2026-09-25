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
	"strings"
	"time"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	goyaml "github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"golang.org/x/crypto/ssh"
)

// metadataPath is where a cluster configuration keeps its identifying information, and
// encryptionKey is the key under it that this file writes and reads.
//
// The record sits in metadata rather than beside the credentials in spec.config because it
// describes the document, not any one registry: every credential in the file is encrypted to the
// same recipients, since one pass over the file writes them all.
const (
	metadataPath      = "$.metadata"
	encryptionKey     = "encryption"
	ageRecipientsPath = "$.metadata.encryption.age.recipients"
	ageModifiedPath   = "$.metadata.encryption.age.lastModified"
)

// errNoMetadataBlock reports that there is nowhere to write the record: the document has no
// metadata mapping, or has one written in flow style.
//
// Flow style is the case worth naming. YAML has no block collection inside a "{a: b}", so writing
// the record there produces a document that no longer parses -- the same failure inFlowCollection
// exists to prevent, and one that would land with the credentials already encrypted into the file.
// Declining to record a fact about a file is a much smaller loss than corrupting it.
var errNoMetadataBlock = errors.New("this document has no block-style metadata mapping to record the recipients in")

// now is time.Now, named so a test can pin the timestamp the record is written with. A clock is
// not worth threading through four exported signatures to make one field predictable.
var now = time.Now

// RecordedRecipients returns the age recipients src records having been encrypted to, along with
// when the record was written, and reports false when the document carries no record at all.
//
// What comes back is a claim the document makes about itself and nothing more. An age header names
// no recipient -- the X25519 stanza is deliberately anonymous -- so there is no way to check this
// list against the ciphertext beside it, and nothing in cargoship tries. It is compared against the
// recipients an operator names, so that a mismatch can be reported; it is never used as key
// material, and a rekey never encrypts to a key that came out of here. See
// docs/agent/choice-age-encryption.md.
//
// A record whose shape is not a list of strings is an error rather than an absent record. A caller
// should report it and carry on: the file is still encrypted correctly, it is only this note about
// it that has been damaged.
func RecordedRecipients(src []byte) ([]string, string, bool, error) {
	if isCRLFDocument(src) {
		src = bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))
	}

	node, _, ok, err := nodeAtPath(src, ageRecipientsPath)
	if err != nil || !ok {
		return nil, "", false, err
	}

	sequence, ok := node.(*ast.SequenceNode)
	if !ok {
		return nil, "", false, fmt.Errorf("%s holds a %s, not a list of recipients", ageRecipientsPath, strings.ToLower(node.Type().String()))
	}

	recipients := make([]string, 0, len(sequence.Values))
	for _, value := range sequence.Values {
		recipient, err := scalarValue(value, ageRecipientsPath)
		if err != nil {
			return nil, "", false, err
		}
		recipients = append(recipients, recipient)
	}

	// The timestamp is read separately and forgiven if it is missing or odd: it is a convenience on
	// a record that is already only a convenience, and losing it is no reason to report the
	// recipients as unreadable.
	lastModified := ""
	if node, _, ok, err := nodeAtPath(src, ageModifiedPath); err == nil && ok {
		if value, err := scalarValue(node, ageModifiedPath); err == nil {
			lastModified = value
		}
	}
	return recipients, lastModified, true, nil
}

// SameRecipients reports whether two lists name the same set of age public keys.
//
// Order is not part of the comparison. A recipients file's order is the order a person wrote their
// team's keys in, which is worth keeping in the record, but it says nothing about who can read the
// file -- so reporting a reordered list as drift would be noise over a difference that is not one.
//
// Nor is an SSH key's authorized_keys comment. The same key given once as "ssh-ed25519 AAAA..." and
// once as "ssh-ed25519 AAAA... alice@laptop" is one key, and the comment is kept in the record
// because it says whose key it is, not because it distinguishes two of them.
func SameRecipients(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	counts := map[string]int{}
	for _, recipient := range a {
		counts[recipientKey(recipient)]++
	}
	for _, recipient := range b {
		key := recipientKey(recipient)
		counts[key]--
		if counts[key] < 0 {
			return false
		}
	}
	return true
}

// recipientKey returns the part of a recipient string that identifies the key, with an SSH key's
// options and trailing comment removed.
//
// The parse is ssh.ParseAuthorizedKey's, the same one agessh uses to accept these keys in the first
// place, so the two agree on what counts as one key by construction. Anything it does not
// recognise -- a native "age1..." recipient, most of all -- is its own key, compared as written.
func recipientKey(recipient string) string {
	trimmed := strings.TrimSpace(recipient)
	if trimmed == "" || strings.HasPrefix(trimmed, "age1") {
		return trimmed
	}

	pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(trimmed))
	if err != nil {
		return trimmed
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pubKey)))
}

// recordRecipients returns doc with the age recipient record brought up to date, and doc unchanged
// when there is nowhere to put one.
//
// Every failure is swallowed, deliberately. The credentials in doc are encrypted correctly by the
// time this runs, and refusing to hand that back because a note about them could not be written
// would throw away the work the command was actually asked to do. What goes wrong here is instead
// caught where it can be reported without losing anything: encrypt-file reads the record back and
// warns when a document it just encrypted has none.
func recordRecipients(doc []byte, k *Keyring) []byte {
	recipients := k.RecipientStrings()
	if len(recipients) == 0 {
		return doc
	}

	recorded, err := writeRecipientRecord(doc, recipients, now())
	if err != nil {
		return doc
	}
	return recorded
}

// stripRecordWhenNoAge returns doc with the recipient record removed, unless doc still holds age
// ciphertext somewhere.
//
// The check is over the whole document rather than over the registry credentials, because
// encrypt-path will put age ciphertext anywhere an operator points it. A record naming the
// recipients of a value that is still encrypted is still true, and removing it would leave that
// value's owner with no note of which key opens it.
func stripRecordWhenNoAge(doc []byte) []byte {
	if bytes.Contains(doc, []byte(cluster.AgeHeader)) {
		return doc
	}

	stripped, err := stripRecipientRecord(doc)
	if err != nil {
		return doc
	}
	return stripped
}

// writeRecipientRecord returns src with metadata.encryption.age holding recipients and a timestamp,
// replacing any record already there.
//
// Like every other rewrite in this package it is a byte-level splice rather than a re-rendering:
// only the lines the record occupies are replaced, so comments, key order and quoting everywhere
// else in the operator's file survive untouched. See EncryptAtPath for why that matters.
func writeRecipientRecord(src []byte, recipients []string, at time.Time) ([]byte, error) {
	return onLineFeeds(src, func(src []byte) ([]byte, error) {
		return writeRecipientRecordLF(src, recipients, at)
	})
}

func writeRecipientRecordLF(src []byte, recipients []string, at time.Time) ([]byte, error) {
	text := string(src)
	lines := lineOffsets(text)

	values, err := metadataValues(src)
	if err != nil {
		return nil, err
	}

	// Replacing an existing record rather than adding a second one. The key's own line is the
	// header the block hangs from, which is what blockEndOffset measures against.
	if existing, ok := mappingValueNamed(values, encryptionKey); ok {
		line := existing.Key.GetToken().Position.Line
		start := lines[line-1]
		end := blockEndOffset(text, lines, line)
		indent := indentOf(text[start:lineEndOffset(text, lines, line)])
		return []byte(text[:start] + renderRecipientRecord(indent, recipients, at, lineEndingAt(text, lines, line)) + text[end:]), nil
	}

	// Appended after the last entry metadata already has, so that the key an operator reads a
	// configuration by stays at the top of the block rather than being pushed down by bookkeeping.
	last := values[len(values)-1]
	lastLine := last.Key.GetToken().Position.Line
	end := blockEndOffset(text, lines, lastLine)

	firstLine := values[0].Key.GetToken().Position.Line
	indent := indentOf(text[lines[firstLine-1]:lineEndOffset(text, lines, firstLine)])
	eol := lineEndingAt(text, lines, lastLine)

	return []byte(text[:end] + eol + renderRecipientRecord(indent, recipients, at, eol) + text[end:]), nil
}

// stripRecipientRecord returns src with the metadata.encryption block removed, and src unchanged
// when there is none.
//
// Decrypting a document removes it because what it records is no longer true of the file: there is
// no ciphertext left for the recipients to be the recipients of. Leaving it would put a list of
// public keys above a file full of plaintext, which reads as a file that is still protected.
func stripRecipientRecord(src []byte) ([]byte, error) {
	return onLineFeeds(src, stripRecipientRecordLF)
}

func stripRecipientRecordLF(src []byte) ([]byte, error) {
	text := string(src)
	lines := lineOffsets(text)

	values, err := metadataValues(src)
	if err != nil {
		// A document with nowhere to keep a record has none to remove. Reporting that as a failure
		// would turn a decrypt that did its whole job into an error.
		if errors.Is(err, errNoMetadataBlock) {
			return src, nil
		}
		return nil, err
	}

	existing, ok := mappingValueNamed(values, encryptionKey)
	if !ok {
		return src, nil
	}

	line := existing.Key.GetToken().Position.Line
	start := lines[line-1]
	end := blockEndOffset(text, lines, line)

	// The line ending after the block goes with it, so removing the record leaves no blank line
	// where it was. A final line without one simply has nothing to consume.
	if end < len(text) && text[end] == '\r' {
		end++
	}
	if end < len(text) && text[end] == '\n' {
		end++
	}
	return []byte(text[:start] + text[end:]), nil
}

// metadataValues returns the entries of the document's metadata mapping, in document order.
//
// A mapping in flow style is refused rather than returned, because every caller here is about to
// write a block into it. That check belongs here rather than at each call site: there is no useful
// thing to do with a flow-style metadata mapping in this file.
func metadataValues(src []byte) ([]*ast.MappingValueNode, error) {
	node, _, ok, err := nodeAtPath(src, metadataPath)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errNoMetadataBlock
	}

	mapping, ok := node.(*ast.MappingNode)
	if !ok || mapping.IsFlowStyle || len(mapping.Values) == 0 {
		return nil, errNoMetadataBlock
	}
	return mapping.Values, nil
}

// mappingValueNamed returns the entry whose key is name.
func mappingValueNamed(values []*ast.MappingValueNode, name string) (*ast.MappingValueNode, bool) {
	for _, value := range values {
		if value.Key.GetToken().Value == name {
			return value, true
		}
	}
	return nil, false
}

// renderRecipientRecord returns the record's lines, indented to sit among metadata's other entries
// and joined with eol. There is no terminator on the last line: the caller splices this over a span
// that stops where a line's content does, and writes back the terminator the document already had.
func renderRecipientRecord(indent int, recipients []string, at time.Time, eol string) string {
	pad := strings.Repeat(" ", indent)
	step := "  "

	out := []string{
		pad + encryptionKey + ":",
		pad + step + "age:",
		pad + step + step + "recipients:",
	}
	for _, recipient := range recipients {
		out = append(out, pad+step+step+step+"- "+renderInlineScalar(recipient))
	}
	// Always quoted. RFC 3339 is full of colons, and a plain scalar holding them is a shape YAML is
	// free to read as something other than a string.
	out = append(out, pad+step+step+"lastModified: "+quoteScalar(at.UTC().Format(time.RFC3339)))

	return strings.Join(out, eol)
}

// renderInlineScalar returns value in the narrowest YAML style that reads back unchanged on a
// single line, which for a public key is almost always the plain one.
//
// go-yaml's encoder picks the style, for the reason renderScalar defers to it: the quoting rules
// are the library's job. Anything it spreads over more than one line is quoted instead, since a
// sequence entry written here has to stay on its own line.
func renderInlineScalar(value string) string {
	encoded, err := goyaml.Marshal(value)
	if err != nil {
		return quoteScalar(value)
	}
	scalar := strings.TrimSuffix(string(encoded), "\n")
	if strings.Contains(scalar, "\n") {
		return quoteScalar(value)
	}
	return scalar
}
