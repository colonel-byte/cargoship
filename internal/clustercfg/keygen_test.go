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
	"strings"
	"testing"

	"filippo.io/age"
)

func TestGenerateAgeIdentityProducesUsableKeys(t *testing.T) {
	id, err := GenerateAgeIdentity()
	if err != nil {
		t.Fatalf("GenerateAgeIdentity() error = %v", err)
	}

	encrypted, err := encryptAge("hunter2", []age.Recipient{id.Recipient()})
	if err != nil {
		t.Fatalf("encryptAge() error = %v", err)
	}
	got, err := decryptAge(encrypted, []age.Identity{id})
	if err != nil {
		t.Fatalf("decryptAge() error = %v", err)
	}
	if got != "hunter2" {
		t.Errorf("round trip = %q, want %q", got, "hunter2")
	}
}

func TestGenerateAgeIdentityReturnsADifferentKeyEachTime(t *testing.T) {
	first, err := GenerateAgeIdentity()
	if err != nil {
		t.Fatalf("GenerateAgeIdentity() error = %v", err)
	}
	second, err := GenerateAgeIdentity()
	if err != nil {
		t.Fatalf("GenerateAgeIdentity() error = %v", err)
	}

	if first.String() == second.String() {
		t.Error("GenerateAgeIdentity() returned the same key twice")
	}
}

// The file has to load through the same parser an operator's age-keygen file loads through, so the
// assertion is the round trip rather than the text.
func TestWriteAgeIdentityParsesBackAsAnIdentity(t *testing.T) {
	id, err := GenerateAgeIdentity()
	if err != nil {
		t.Fatalf("GenerateAgeIdentity() error = %v", err)
	}

	var buf bytes.Buffer
	if err := WriteAgeIdentity(&buf, id); err != nil {
		t.Fatalf("WriteAgeIdentity() error = %v", err)
	}

	identities, err := age.ParseIdentities(&buf)
	if err != nil {
		t.Fatalf("age.ParseIdentities() error = %v", err)
	}
	if len(identities) != 1 {
		t.Fatalf("age.ParseIdentities() returned %d identities, want 1", len(identities))
	}

	parsed, ok := identities[0].(*age.X25519Identity)
	if !ok {
		t.Fatalf("age.ParseIdentities() returned a %T, want *age.X25519Identity", identities[0])
	}
	if parsed.String() != id.String() {
		t.Error("the parsed identity is not the one that was written")
	}
}

// The comment is the only copy of the public key an operator has once the terminal output is gone.
func TestWriteAgeIdentityCommentsThePublicKey(t *testing.T) {
	id, err := GenerateAgeIdentity()
	if err != nil {
		t.Fatalf("GenerateAgeIdentity() error = %v", err)
	}

	var buf bytes.Buffer
	if err := WriteAgeIdentity(&buf, id); err != nil {
		t.Fatalf("WriteAgeIdentity() error = %v", err)
	}

	want := "# public key: " + id.Recipient().String()
	if !strings.Contains(buf.String(), want) {
		t.Errorf("WriteAgeIdentity() = %q, want it to contain %q", buf.String(), want)
	}
	if !strings.HasPrefix(buf.String(), "# created: ") {
		t.Errorf("WriteAgeIdentity() = %q, want it to start with a created comment", buf.String())
	}
}

func TestAgeRecipientsInReadsAWrittenIdentity(t *testing.T) {
	id, err := GenerateAgeIdentity()
	if err != nil {
		t.Fatalf("GenerateAgeIdentity() error = %v", err)
	}

	var buf bytes.Buffer
	if err := WriteAgeIdentity(&buf, id); err != nil {
		t.Fatalf("WriteAgeIdentity() error = %v", err)
	}

	got, err := AgeRecipientsIn(&buf)
	if err != nil {
		t.Fatalf("AgeRecipientsIn() error = %v", err)
	}
	if len(got) != 1 || got[0] != id.Recipient().String() {
		t.Errorf("AgeRecipientsIn() = %v, want [%s]", got, id.Recipient())
	}
}

// An identity file holding several keys is ordinary -- it is how an operator keeps reading values
// encrypted to a key they have rotated away from.
func TestAgeRecipientsInReadsEveryIdentityInTheFile(t *testing.T) {
	var buf bytes.Buffer
	var want []string
	for range 3 {
		id, err := GenerateAgeIdentity()
		if err != nil {
			t.Fatalf("GenerateAgeIdentity() error = %v", err)
		}
		if err := WriteAgeIdentity(&buf, id); err != nil {
			t.Fatalf("WriteAgeIdentity() error = %v", err)
		}
		want = append(want, id.Recipient().String())
	}

	got, err := AgeRecipientsIn(&buf)
	if err != nil {
		t.Fatalf("AgeRecipientsIn() error = %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("AgeRecipientsIn() returned %d recipients, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("AgeRecipientsIn()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestAgeRecipientsInRejectsSomethingThatIsNotAnIdentity(t *testing.T) {
	_, err := AgeRecipientsIn(strings.NewReader("not a key\n"))
	if err == nil {
		t.Fatal("AgeRecipientsIn() error = nil, want an error for a file that holds no identity")
	}
}

// A recipients file is the easy thing to hand this by mistake, and the two formats look alike
// enough that saying nothing would leave an operator reading an empty list as an answer.
func TestAgeRecipientsInRejectsARecipientsFile(t *testing.T) {
	id, err := GenerateAgeIdentity()
	if err != nil {
		t.Fatalf("GenerateAgeIdentity() error = %v", err)
	}

	_, err = AgeRecipientsIn(strings.NewReader(id.Recipient().String() + "\n"))
	if err == nil {
		t.Fatal("AgeRecipientsIn() error = nil, want an error for a recipients file")
	}
}
