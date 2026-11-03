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

package phase

import (
	"unicode/utf8"

	"github.com/pmezard/go-difflib/difflib"
)

// maxDiffInput is the largest file either side of a diff may be.
//
// A run receipt holds one of these per drifted file per host, and a fleet of thirty nodes each
// rewriting a handful of files turns a generous limit into a document nobody can open. Engine
// configuration is a few kilobytes; anything approaching this is not the kind of file a line
// diff helps with anyway.
const maxDiffInput = 64 << 10

// diffContext is how many unchanged lines surround each hunk. Three is what diff -u defaults to
// and what every reader of a patch expects.
const diffContext = 3

// unifiedDiff renders the change from the host's copy of a file to the desired one.
//
// It returns empty rather than a partial answer whenever a line diff would not be meaningful:
// content that is not valid UTF-8 is binary as far as this is concerned, and content past
// maxDiffInput is too large to carry. An empty diff always means "not compared", never "no
// difference" -- the caller only asks once it already knows the two differ.
//
// The result can contain anything the file contained, which for an engine configuration includes
// join tokens and registry credentials. It belongs in a receipt that stays on the management
// node, and is dropped by anything that renders one for sharing.
func unifiedDiff(path, current, desired string) string {
	if len(current) > maxDiffInput || len(desired) > maxDiffInput {
		return ""
	}
	if !utf8.ValidString(current) || !utf8.ValidString(desired) {
		return ""
	}

	out, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(current),
		B:        difflib.SplitLines(desired),
		FromFile: path,
		FromDate: "on the host",
		ToFile:   path,
		ToDate:   "desired",
		Context:  diffContext,
	})
	if err != nil {
		// The only failure mode is a write to the internal buffer, which cannot fail here.
		// Report nothing rather than surfacing an error for a field that is already optional.
		return ""
	}
	return out
}
