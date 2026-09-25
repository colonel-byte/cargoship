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
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/internal/clustercfg"
	"github.com/stretchr/testify/require"
)

// FuzzParseRecipient asserts that ParseRecipient never panics on a single public-key line,
// whatever prefix it starts with, and that success always yields a non-nil recipient.
//
// The function dispatches by string prefix -- "age1" to the native parser, a secret-key prefix
// to an outright refusal, anything else to the SSH authorized_keys parser -- before either parser
// ever sees the string. That dispatch, not either parser, is what is worth fuzzing: a line
// crafted to straddle two prefixes is exactly the input that would misroute.
func FuzzParseRecipient(f *testing.F) {
	f.Add("age1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq")
	f.Add("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBaJnNgQq6tw5F0aeqhPFYP1sVI7z0F9L+e1EIPHNBhL")
	f.Add("AGE-SECRET-KEY-1QQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQ")
	f.Add("-----BEGIN OPENSSH PRIVATE KEY-----")
	f.Add("")
	f.Add("age1")
	f.Add("age1pq1notreallyvalid")
	f.Add("not a key at all")
	f.Add("ssh-ed25519")
	f.Add("age1-----BEGIN ")

	f.Fuzz(func(t *testing.T, key string) {
		recipient, err := clustercfg.ParseRecipient(key)
		if err != nil {
			require.Nil(t, recipient, "an error result still carried a recipient for %q", key)
			return
		}
		require.NotNil(t, recipient, "a successful result carried no recipient for %q", key)
	})
}

// FuzzParseRecipients asserts that ParseRecipients, which reads a whole recipients file line by
// line dispatching each to ParseRecipient, never panics and that every line it accepts comes back
// paired with the recipient it parsed.
//
// This is the counterpart to AgeRecipientsIn (identity-file parsing, already fuzzed elsewhere):
// a recipients file mixes native age keys, SSH authorized_keys lines, comments, and blank lines,
// so the property worth holding it to is that the recipients and lines it returns stay the same
// length and in step with each other, not just that the scan itself does not panic.
func FuzzParseRecipients(f *testing.F) {
	f.Add("age1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq\nssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBaJnNgQq6tw5F0aeqhPFYP1sVI7z0F9L+e1EIPHNBhL\n")
	f.Add("")
	f.Add("# just a comment\n")
	f.Add("\n\n\n")
	f.Add("AGE-SECRET-KEY-1QQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQ\n")
	f.Add("not a key at all\n")
	f.Add("age1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq\nnot a key at all\n")
	f.Add(strings.Repeat("# comment\n", 100))
	f.Add("\xff\xfe not valid utf-8\n")
	f.Add("age1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq")

	f.Fuzz(func(t *testing.T, content string) {
		recipients, lines, err := clustercfg.ParseRecipients(strings.NewReader(content))
		if err != nil {
			return
		}
		require.Len(t, lines, len(recipients), "recipients and lines came back different lengths for %q", content)
		for i, line := range lines {
			require.NotEmpty(t, line, "line %d in the result is empty for %q", i, content)
			require.NotNil(t, recipients[i], "recipient %d in the result is nil for %q", i, content)
		}
	})
}
