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

	"github.com/colonel-byte/cargoship/src/pkg/phase"
	"github.com/stretchr/testify/require"
)

// FuzzParseManifest asserts that ParseManifest, which reads back the upload-manifest content a
// remote host holds, never panics and that every entry it returns is one a line of content could
// actually have produced.
//
// The manifest is read back from wherever cargoship uploaded it, which is not something cargoship
// fully controls once it is written -- so this is content that could have been modified or
// truncated on the host, not just content cargoship wrote itself. Malformed lines are meant to be
// skipped rather than fail the whole parse, and the property worth holding it to is that a
// category never carries the separator that is supposed to end it.
func FuzzParseManifest(f *testing.F) {
	f.Add("engine\t/usr/local/bin/k3s\nimage\t/var/lib/rancher/k3s/agent/images/foo.tar\n")
	f.Add("")
	f.Add("\n\n\n")
	f.Add("no-separator-on-this-line")
	f.Add("category\t")
	f.Add("\tpath-with-no-category")
	f.Add("category\tpath\textra-tab")
	f.Add("  category\tpath-with-leading-whitespace  \n")
	f.Add("category\tpath\r\ncategory2\tpath2\r\n")
	f.Add(strings.Repeat("category\tpath\n", 100))

	f.Fuzz(func(t *testing.T, content string) {
		entries := phase.ParseManifest(content)
		for _, e := range entries {
			require.NotContains(t, e.Category, "\t", "a category retained the manifest separator for content %q", content)
		}
	})
}
