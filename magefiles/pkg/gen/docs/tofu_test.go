// Copyright 2026 colonel-byte
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package docs

import (
	"strings"
	"testing"
)

// TestTofuSummaryListsEveryPage holds the book's table of contents against the pages the generator
// writes. A page written and not listed is a page nothing links to, which is how a reference
// quietly stops being read.
func TestTofuSummaryListsEveryPage(t *testing.T) {
	lines, err := tofuSummary()
	if err != nil {
		t.Fatalf("tofuSummary reported %v", err)
	}
	if len(lines) == 0 {
		t.Fatal("the OpenTofu section of SUMMARY.md would be empty")
	}

	// The pages the provider serves today. Named rather than counted, so adding a resource and
	// forgetting the summary fails here with the name of what is missing.
	for _, want := range []string{
		// keep-sorted start
		"tofu/data-sources/cluster_facts.md",
		"tofu/index.md",
		"tofu/resources/cluster.md",
		// keep-sorted end
	} {
		var found bool
		for _, line := range lines {
			if strings.Contains(line, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("the summary does not list %s", want)
		}
	}
}
