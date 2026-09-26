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

	"github.com/colonel-byte/cargoship/cmd"
	"github.com/stretchr/testify/require"
)

// FuzzParseRegistryOverrides asserts that ParseRegistryOverrides, which turns repeated
// --registry-override flags into a structured, deduped, sorted list, never panics and that on
// success its two stated invariants hold: no two entries share a Source, and the entries are
// sorted so the longest Source (and so the most specific prefix match) sorts first.
//
// The fuzzed string is split on newlines into the flag's repeated-value form, which is how
// []string-typed CLI input is reached from the string/[]byte/numeric types fuzzing supports.
func FuzzParseRegistryOverrides(f *testing.F) {
	f.Add("docker.io/library=docker.example.com\ndocker.io=docker.example.com")
	f.Add("docker.io=docker.example.com\ndocker.io=other.example.com")
	f.Add("missing-equals")
	f.Add("=missing-source")
	f.Add("missing-override=")
	f.Add("")
	f.Add("a=b\na=c")
	f.Add("=")
	f.Add("a=b=c")
	f.Add(strings.Repeat("x=y\n", 50))

	f.Fuzz(func(t *testing.T, overrides string) {
		var lines []string
		if overrides != "" {
			lines = strings.Split(overrides, "\n")
		}

		result, err := cmd.ParseRegistryOverrides(lines)
		if err != nil {
			return
		}

		seen := make(map[string]bool, len(result))
		for i, o := range result {
			require.NotEmpty(t, o.Source, "entry %d has an empty source", i)
			require.NotEmpty(t, o.Override, "entry %d has an empty override", i)
			require.False(t, seen[o.Source], "source %q appears more than once in the result", o.Source)
			seen[o.Source] = true
			if i > 0 {
				require.GreaterOrEqual(t, result[i-1].Source, o.Source,
					"result is not sorted in descending order by source: %q before %q", result[i-1].Source, o.Source)
			}
		}
	})
}
