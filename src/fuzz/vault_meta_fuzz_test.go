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

package fuzz

import (
	"testing"

	"github.com/colonel-byte/cargoship/internal/clustercfg"
)

// FuzzRecordedRecipientsNoPanic asserts that RecordedRecipients never panics on arbitrary
// configuration bytes.
//
// It reads the age-recipient record with a hand-rolled AST walk over whatever the document's
// metadata.encryption.age block turns out to hold, ahead of any other validation -- a corrupted or
// adversarial configuration file reaches this before its shape is checked anywhere else.
func FuzzRecordedRecipientsNoPanic(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("metadata:\n  encryption:\n    age:\n      recipients:\n        - age1abc\n      lastModified: \"2024-01-01T00:00:00Z\"\n"))
	f.Add([]byte("metadata:\n  encryption:\n    age:\n      recipients: not-a-list\n"))
	f.Add([]byte("metadata:\n  encryption:\n    age:\n      recipients:\n        - {not: a-scalar}\n"))
	f.Add([]byte("metadata: not-a-mapping\n"))
	f.Add([]byte("metadata: {}\n"))
	f.Add([]byte("metadata:\n  encryption: &x\n    age: *x\n"))
	f.Add([]byte("metadata:\r\n  encryption:\r\n    age:\r\n      recipients:\r\n        - age1abc\r\n"))
	f.Add([]byte("not yaml: [unterminated"))
	f.Add([]byte("just a scalar"))

	f.Fuzz(func(_ *testing.T, doc []byte) {
		_, _, _, _ = clustercfg.RecordedRecipients(doc) //nolint:errcheck // no-panic fuzz test, malformed input is expected to error
	})
}
