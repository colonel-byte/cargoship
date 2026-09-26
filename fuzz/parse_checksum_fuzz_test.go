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

	"github.com/colonel-byte/cargoship/pkg/utils"
	"github.com/stretchr/testify/require"
)

// FuzzParseChecksum asserts that ParseChecksum never panics and that the src/checksum split it
// returns can always be rejoined into the original string.
//
// The function decides whether a trailing "@..." is a checksum suffix or a URL's userinfo by
// counting '@' symbols and re-parsing as a URL, then falls back to index-based slicing. That is
// exactly the kind of hand-rolled logic that panics on an out-of-range index or misclassifies a
// pathological URL (one with multiple '@' symbols, or a scheme-less string that happens to parse).
func FuzzParseChecksum(f *testing.F) {
	f.Add("https://example.com/file.tar.zst@sha256:abcdef")
	f.Add("https://user@example.com/file.tar.zst")
	f.Add("https://user:pass@example.com/file.tar.zst@sha256:abcdef")
	f.Add("no-checksum-no-at-symbol")
	f.Add("@")
	f.Add("@@@")
	f.Add("://@@@")
	f.Add("")
	f.Add("not a url@but has an at symbol")
	f.Add("oci://ghcr.io/example/package:v1@sha256:abcdef")

	f.Fuzz(func(t *testing.T, src string) {
		original := src
		stripped, checksum, err := utils.ParseChecksum(src)
		if err != nil {
			require.Empty(t, checksum, "an error result still carried a checksum for %q", original)
			require.Equal(t, original, stripped, "an error result still changed src for %q", original)
			return
		}

		if stripped == original {
			require.Empty(t, checksum, "src was left unchanged but a checksum was still returned for %q", original)
			return
		}
		require.Equal(t, original, stripped+"@"+checksum,
			"src %q and checksum %q do not rejoin into the original %q", stripped, checksum, original)
	})
}

// FuzzParseRetryAfterNoPanic asserts that ParseRetryAfter never panics, whatever string a
// registry or mirror sent back in a Retry-After header.
//
// The only caller treats a non-positive result as "no backoff" (`d > 0`), so a negative or zero
// duration from a malformed or adversarial header is already handled correctly downstream; the
// property worth fuzzing is that ParseInt's overflow and http.ParseTime's own parsing never
// panic on it.
func FuzzParseRetryAfterNoPanic(f *testing.F) {
	f.Add("120")
	f.Add("0")
	f.Add("-5")
	f.Add("")
	f.Add("Wed, 21 Oct 2015 07:28:00 GMT")
	f.Add("not a valid value")
	f.Add(strings.Repeat("9", 100))
	f.Add("9999999999999999999999999999999999999999")

	f.Fuzz(func(_ *testing.T, value string) {
		_ = utils.ParseRetryAfter(value)
	})
}
