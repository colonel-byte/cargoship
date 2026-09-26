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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	goyaml "github.com/goccy/go-yaml"
)

const flowMetadataInventoryFixture = "../../test/e2e/noncluster/testdata/inventory-flow-metadata.yaml"

func readFlowMetadataInventoryFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(flowMetadataInventoryFixture)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", flowMetadataInventoryFixture, err)
	}
	return data
}

// pinClock fixes the timestamp the record is written with, so a test can compare two documents byte
// for byte without the second one differing only in the second it ran in.
func pinClock(t *testing.T, at time.Time) {
	t.Helper()
	previous := now
	now = func() time.Time { return at }
	t.Cleanup(func() { now = previous })
}

// TestEncryptConfigRecordsRecipients is the point of the whole file: after encrypting, the document
// says which keys it was encrypted to, which is the one thing the ciphertext itself cannot say.
func TestEncryptConfigRecordsRecipients(t *testing.T) {
	pinClock(t, time.Date(2026, 9, 16, 10, 30, 0, 0, time.UTC))
	k := newAgeKeyring(t)

	got, changed, _, err := EncryptConfig(readVaultInventoryFixture(t), k, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	if len(changed) == 0 {
		t.Fatal("EncryptConfig() changed nothing, so there was nothing to record")
	}

	recorded, lastModified, ok, err := RecordedRecipients(got)
	if err != nil {
		t.Fatalf("RecordedRecipients() error = %v", err)
	}
	if !ok {
		t.Fatalf("RecordedRecipients() found no record in:\n%s", got)
	}
	if want := k.RecipientStrings(); !slices.Equal(recorded, want) {
		t.Errorf("recorded = %q, want %q", recorded, want)
	}
	if lastModified != "2026-09-16T10:30:00Z" {
		t.Errorf("lastModified = %q, want the time the record was written", lastModified)
	}

	// The record is part of the document an apply reads, so it has to decode into the spec types
	// rather than only into the YAML parser this package splices with.
	var dis cluster.ZarfCluster
	if err := goyaml.Unmarshal(got, &dis); err != nil {
		t.Fatalf("unmarshalling:\n%s\nerror = %v", got, err)
	}
	if dis.Metadata.Encryption == nil || dis.Metadata.Encryption.Age == nil {
		t.Fatalf("metadata.encryption.age did not decode from:\n%s", got)
	}
	if !slices.Equal(dis.Metadata.Encryption.Age.Recipients, k.RecipientStrings()) {
		t.Errorf("decoded recipients = %q, want %q", dis.Metadata.Encryption.Age.Recipients, k.RecipientStrings())
	}
	if dis.Metadata.Name != "test" {
		t.Errorf("metadata.name = %q, want the record to have left it alone", dis.Metadata.Name)
	}
}

// TestEncryptConfigRecordsNothingForVault holds the boundary: the record names age recipients, and
// a vaulted document has none. One password covers such a file and the operator names it every
// time, so there is nothing about it the file could usefully record.
func TestEncryptConfigRecordsNothingForVault(t *testing.T) {
	got, changed, _, err := EncryptConfig(readVaultInventoryFixture(t), testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	if len(changed) == 0 {
		t.Fatal("EncryptConfig() changed nothing")
	}

	if _, _, ok, err := RecordedRecipients(got); err != nil || ok {
		t.Errorf("RecordedRecipients() = ok %v, err %v, want no record in a vaulted document", ok, err)
	}
	if strings.Contains(string(got), "encryption:") {
		t.Errorf("a vaulted document was given an encryption block:\n%s", got)
	}
}

// TestEncryptConfigLeavesTheRecordAloneWhenNothingChanged pins the decision that a run which
// encrypts nothing writes nothing.
//
// Without it the second run would rewrite lastModified and the file would come back changed, which
// would make "running it twice changes nothing" false for the one field this feature added -- and
// would produce a write where finishVaultFile reports there was nothing to do.
func TestEncryptConfigLeavesTheRecordAloneWhenNothingChanged(t *testing.T) {
	pinClock(t, time.Date(2026, 9, 16, 10, 30, 0, 0, time.UTC))
	k := newAgeKeyring(t)

	once, _, _, err := EncryptConfig(readVaultInventoryFixture(t), k, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	// The clock moves between the runs, so a record rewritten by the second one cannot come back
	// identical by luck.
	pinClock(t, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))

	twice, changed, _, err := EncryptConfig(once, k, false)
	if err != nil {
		t.Fatalf("EncryptConfig() second pass error = %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("changed = %v, want nothing on the second pass", changed)
	}
	if string(twice) != string(once) {
		t.Errorf("the second pass rewrote the document:\n%s", twice)
	}
}

// TestWriteRecipientRecordUpdatesAnExistingBlock covers the replace path rather than the insert
// one: a second record has to take the first one's place, not sit beside it.
func TestWriteRecipientRecordUpdatesAnExistingBlock(t *testing.T) {
	at := time.Date(2026, 9, 16, 10, 30, 0, 0, time.UTC)

	first, err := writeRecipientRecord(readVaultInventoryFixture(t), []string{"age1first"}, at)
	if err != nil {
		t.Fatalf("writeRecipientRecord() error = %v", err)
	}
	second, err := writeRecipientRecord(first, []string{"age1second", "age1third"}, at)
	if err != nil {
		t.Fatalf("writeRecipientRecord() second error = %v", err)
	}

	if count := strings.Count(string(second), "encryption:"); count != 1 {
		t.Errorf("document holds %d encryption blocks, want 1:\n%s", count, second)
	}
	recorded, _, ok, err := RecordedRecipients(second)
	if err != nil || !ok {
		t.Fatalf("RecordedRecipients() = ok %v, err %v", ok, err)
	}
	if want := []string{"age1second", "age1third"}; !slices.Equal(recorded, want) {
		t.Errorf("recorded = %q, want %q", recorded, want)
	}

	// Writing the first record again has to land back on the first document exactly, which is what
	// says the replace spans the same bytes the insert wrote.
	back, err := writeRecipientRecord(second, []string{"age1first"}, at)
	if err != nil {
		t.Fatalf("writeRecipientRecord() third error = %v", err)
	}
	if string(back) != string(first) {
		t.Errorf("replacing the record did not return the document to its earlier shape:\n%s", back)
	}
}

// TestWriteRecipientRecordPreservesTheRestOfTheDocument is the property every rewrite in this
// package has: only the record's own lines move. Stripping it again has to give back the file byte
// for byte, comments and all.
func TestWriteRecipientRecordPreservesTheRestOfTheDocument(t *testing.T) {
	at := time.Date(2026, 9, 16, 10, 30, 0, 0, time.UTC)

	for _, fixture := range []string{vaultInventoryFixture, pathsInventoryFixture, anchoredInventoryFixture, flowInventoryFixture} {
		t.Run(fixture, func(t *testing.T) {
			src, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatalf("reading fixture: %v", err)
			}

			recorded, err := writeRecipientRecord(src, []string{"age1abc", "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIP4H alice@laptop"}, at)
			if err != nil {
				t.Fatalf("writeRecipientRecord() error = %v", err)
			}
			// A document that has only gained a metadata block still has to parse as one, or the
			// next command to walk it fails on a line the operator never wrote.
			if _, _, err := credentialPathsFor(recorded); err != nil {
				t.Fatalf("the recorded document no longer walks: %v\n%s", err, recorded)
			}

			stripped, err := stripRecipientRecord(recorded)
			if err != nil {
				t.Fatalf("stripRecipientRecord() error = %v", err)
			}
			if string(stripped) != string(src) {
				t.Errorf("writing and stripping the record did not restore the document:\n%s", stripped)
			}
		})
	}
}

// credentialPathsFor is credentialPaths plus a read of every value it names, which is what the
// whole-file commands do and so is what "the document still walks" means here.
func credentialPathsFor(doc []byte) ([]string, []string, error) {
	paths, err := credentialPaths(doc)
	if err != nil {
		return nil, nil, err
	}
	values := make([]string, 0, len(paths))
	for _, path := range paths {
		value, _, err := scalarAtPath(doc, path)
		if err != nil {
			return nil, nil, err
		}
		values = append(values, value)
	}
	return paths, values, nil
}

// TestWriteRecipientRecordRefusesFlowMetadata holds the one shape the record cannot be written
// into. YAML has no block collection inside a "{a: b}", so splicing one there leaves a document
// that no longer parses -- with the credentials already encrypted into it. Declining to record a
// fact about a file is much the smaller loss.
func TestWriteRecipientRecordRefusesFlowMetadata(t *testing.T) {
	src := readFlowMetadataInventoryFixture(t)

	if _, err := writeRecipientRecord(src, []string{"age1abc"}, time.Now()); err == nil {
		t.Fatal("writeRecipientRecord() error = nil, want a refusal")
	}

	// Encrypting such a file still works, and still writes the credentials. Only the note about
	// them is declined.
	k := newAgeKeyring(t)
	got, changed, _, err := EncryptConfig(src, k, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	if len(changed) == 0 {
		t.Fatal("EncryptConfig() changed nothing, so the refusal was never reached")
	}
	for _, path := range changed {
		if value := readPath(t, got, path); !cluster.IsAgeEncrypted(value) {
			t.Errorf("value at %s = %q, want age ciphertext", path, value)
		}
	}
	if _, _, ok, err := RecordedRecipients(got); err != nil || ok {
		t.Errorf("RecordedRecipients() = ok %v, err %v, want no record in a flow-style metadata mapping:\n%s", ok, err, got)
	}
}

// TestDecryptConfigStripsTheRecord covers the removal: a list of public keys above a document full
// of plaintext reads as a file that is still protected, which is the one thing it must not say.
func TestDecryptConfigStripsTheRecord(t *testing.T) {
	k := newAgeKeyring(t)
	src := readVaultInventoryFixture(t)

	encrypted, _, _, err := EncryptConfig(src, k, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	if _, _, ok, err := RecordedRecipients(encrypted); err != nil || !ok {
		t.Fatalf("EncryptConfig() wrote no record (ok %v, err %v), so there is nothing to strip", ok, err)
	}

	decrypted, _, _, err := DecryptConfig(encrypted, k)
	if err != nil {
		t.Fatalf("DecryptConfig() error = %v", err)
	}
	if _, _, ok, err := RecordedRecipients(decrypted); err != nil || ok {
		t.Errorf("RecordedRecipients() = ok %v, err %v, want the record gone after a decrypt:\n%s", ok, err, decrypted)
	}
	if string(decrypted) != string(src) {
		t.Errorf("a round trip through age did not restore the document:\n%s", decrypted)
	}
}

// TestDecryptConfigKeepsTheRecordWhileAgeCiphertextRemains is why the strip is conditional: a value
// encrypted somewhere encrypt-path was pointed at is still age ciphertext, and its owner still
// needs the note saying which key opens it.
func TestDecryptConfigKeepsTheRecordWhileAgeCiphertextRemains(t *testing.T) {
	k := newAgeKeyring(t)

	encrypted, _, _, err := EncryptConfig(readVaultInventoryFixture(t), k, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}
	// A value outside the registry credentials the whole-file commands walk, which is exactly what
	// encrypt-path is for.
	encrypted, err = EncryptAtPath(encrypted, "$.spec.config.loadbalancer", k, false)
	if err != nil {
		t.Fatalf("EncryptAtPath() error = %v", err)
	}

	decrypted, _, _, err := DecryptConfig(encrypted, k)
	if err != nil {
		t.Fatalf("DecryptConfig() error = %v", err)
	}
	if _, _, ok, err := RecordedRecipients(decrypted); err != nil || !ok {
		t.Errorf("RecordedRecipients() = ok %v, err %v, want the record kept while age ciphertext remained:\n%s", ok, err, decrypted)
	}
}

// TestRekeyConfigRecordsMovingToAge is the migration path: a vaulted file read with its password
// and written to age recipients comes out naming them.
func TestRekeyConfigRecordsMovingToAge(t *testing.T) {
	to := newAgeKeyring(t)

	vaulted, _, _, err := EncryptConfig(readVaultInventoryFixture(t), testKeyring, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	rekeyed, changed, _, err := RekeyConfig(vaulted, testKeyring, to)
	if err != nil {
		t.Fatalf("RekeyConfig() error = %v", err)
	}
	if len(changed) == 0 {
		t.Fatal("RekeyConfig() changed nothing")
	}

	recorded, _, ok, err := RecordedRecipients(rekeyed)
	if err != nil || !ok {
		t.Fatalf("RecordedRecipients() = ok %v, err %v", ok, err)
	}
	if want := to.RecipientStrings(); !slices.Equal(recorded, want) {
		t.Errorf("recorded = %q, want %q", recorded, want)
	}
}

// TestRekeyConfigStripsTheRecordMovingToVault is the other direction, and the reason the strip is
// not only a decrypt concern: a list naming keys nothing in the file is encrypted to any more is
// worse than no list at all.
func TestRekeyConfigStripsTheRecordMovingToVault(t *testing.T) {
	from := newAgeKeyring(t)

	encrypted, _, _, err := EncryptConfig(readVaultInventoryFixture(t), from, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	rekeyed, changed, _, err := RekeyConfig(encrypted, from, testKeyring)
	if err != nil {
		t.Fatalf("RekeyConfig() error = %v", err)
	}
	if len(changed) == 0 {
		t.Fatal("RekeyConfig() changed nothing")
	}
	if _, _, ok, err := RecordedRecipients(rekeyed); err != nil || ok {
		t.Errorf("RecordedRecipients() = ok %v, err %v, want the record gone after a move onto Ansible Vault:\n%s", ok, err, rekeyed)
	}
}

// TestRekeyConfigRecordsTheNewRecipients covers rotation rather than migration: the record has to
// follow the keys the file is now readable with, not the ones it used to be.
func TestRekeyConfigRecordsTheNewRecipients(t *testing.T) {
	from := newAgeKeyring(t)
	to := newAgeKeyring(t)

	encrypted, _, _, err := EncryptConfig(readVaultInventoryFixture(t), from, false)
	if err != nil {
		t.Fatalf("EncryptConfig() error = %v", err)
	}

	rekeyed, _, _, err := RekeyConfig(encrypted, from, to)
	if err != nil {
		t.Fatalf("RekeyConfig() error = %v", err)
	}

	recorded, _, ok, err := RecordedRecipients(rekeyed)
	if err != nil || !ok {
		t.Fatalf("RecordedRecipients() = ok %v, err %v", ok, err)
	}
	if want := to.RecipientStrings(); !slices.Equal(recorded, want) {
		t.Errorf("recorded = %q, want the new recipients %q", recorded, want)
	}
}

func TestRecordedRecipientsReportsAnAbsentRecord(t *testing.T) {
	recipients, lastModified, ok, err := RecordedRecipients(readVaultInventoryFixture(t))
	if err != nil {
		t.Fatalf("RecordedRecipients() error = %v", err)
	}
	if ok || recipients != nil || lastModified != "" {
		t.Errorf("RecordedRecipients() = %q, %q, %v, want an absent record", recipients, lastModified, ok)
	}
}

// TestRecordedRecipientsReportsAMalformedRecord covers a hand-edited file. It is an error rather
// than an absent record, so that a caller can say the note is damaged instead of quietly reporting
// a file that claims nothing.
func TestRecordedRecipientsReportsAMalformedRecord(t *testing.T) {
	src := []byte("apiVersion: zarf.dev/v1alpha1\nkind: ZarfCluster\nmetadata:\n  name: test\n  encryption:\n    age:\n      recipients: age1abc\n")

	if _, _, ok, err := RecordedRecipients(src); err == nil || ok {
		t.Errorf("RecordedRecipients() = ok %v, err %v, want an error naming the shape", ok, err)
	}
}

// TestStripRecipientRecordLeavesADocumentWithoutOneAlone keeps the decrypt path honest: removing
// nothing has to be a no-op rather than a rewrite or a failure.
func TestStripRecipientRecordLeavesADocumentWithoutOneAlone(t *testing.T) {
	for _, fixture := range []string{vaultInventoryFixture, flowMetadataInventoryFixture} {
		t.Run(fixture, func(t *testing.T) {
			src, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatalf("reading fixture: %v", err)
			}
			got, err := stripRecipientRecord(src)
			if err != nil {
				t.Fatalf("stripRecipientRecord() error = %v", err)
			}
			if string(got) != string(src) {
				t.Errorf("stripRecipientRecord() rewrote a document with no record:\n%s", got)
			}
		})
	}
}

// TestSameRecipientsComparesKeysRatherThanText holds what counts as drift. Two lists naming the
// same keys are not drift however they are spelled or ordered, because the question the comparison
// answers is who can read the file.
func TestSameRecipientsComparesKeysRatherThanText(t *testing.T) {
	key := newEd25519SSHKey(t)

	tests := []struct {
		name string
		a    []string
		b    []string
		want bool
	}{
		{"identical", []string{"age1a", "age1b"}, []string{"age1a", "age1b"}, true},
		{"reordered", []string{"age1a", "age1b"}, []string{"age1b", "age1a"}, true},
		{"one added", []string{"age1a"}, []string{"age1a", "age1b"}, false},
		{"one replaced", []string{"age1a", "age1b"}, []string{"age1a", "age1c"}, false},
		{"both empty", nil, nil, true},
		{"ssh comment added", []string{key.authorized}, []string{key.authorized + " alice@laptop"}, true},
		{"ssh options added", []string{key.authorized}, []string{"no-agent-forwarding " + key.authorized}, true},
		{"different ssh keys", []string{key.authorized}, []string{newEd25519SSHKey(t).authorized}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SameRecipients(tt.a, tt.b); got != tt.want {
				t.Errorf("SameRecipients() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestWriteRecipientRecordQuotesWhatPlainStyleWouldChange guards the rendering. A recipient is
// nearly always plain-safe, but nothing stops an authorized_keys comment holding a character YAML
// reads as structure, and a record that does not read back as what was written is worse than none.
func TestWriteRecipientRecordQuotesWhatPlainStyleWouldChange(t *testing.T) {
	awkward := []string{
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIP4H alice: the one with #2",
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIP4H  trailing space ",
		"- not really a list entry",
	}

	got, err := writeRecipientRecord(readVaultInventoryFixture(t), awkward, time.Now())
	if err != nil {
		t.Fatalf("writeRecipientRecord() error = %v", err)
	}

	recorded, _, ok, err := RecordedRecipients(got)
	if err != nil || !ok {
		t.Fatalf("RecordedRecipients() = ok %v, err %v, on:\n%s", ok, err, got)
	}
	if !slices.Equal(recorded, awkward) {
		t.Errorf("recorded = %q, want %q", recorded, awkward)
	}
}

// TestWriteRecipientRecordKeepsCRLF covers a file saved by an editor on Windows, which every other
// rewrite in this package handles by converting and converting back.
func TestWriteRecipientRecordKeepsCRLF(t *testing.T) {
	at := time.Date(2026, 9, 16, 10, 30, 0, 0, time.UTC)
	src := []byte(strings.ReplaceAll(string(readVaultInventoryFixture(t)), "\n", "\r\n"))

	got, err := writeRecipientRecord(src, []string{"age1abc"}, at)
	if err != nil {
		t.Fatalf("writeRecipientRecord() error = %v", err)
	}
	if strings.Contains(strings.ReplaceAll(string(got), "\r\n", ""), "\n") {
		t.Errorf("the record was written with bare line feeds into a CRLF document:\n%q", got)
	}
	if _, _, ok, err := RecordedRecipients(got); err != nil || !ok {
		t.Fatalf("RecordedRecipients() = ok %v, err %v", ok, err)
	}

	stripped, err := stripRecipientRecord(got)
	if err != nil {
		t.Fatalf("stripRecipientRecord() error = %v", err)
	}
	if string(stripped) != string(src) {
		t.Errorf("a CRLF document did not survive the round trip:\n%q", stripped)
	}
}
