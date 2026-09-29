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

package osv_test

import (
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/magefiles/pkg/devtools/osv"
)

func TestForeignManifest(t *testing.T) {
	tests := []struct {
		filename string
		expected bool
	}{
		{
			filename: "package-lock.json",
			expected: true,
		},
		{
			filename: "yarn.lock",
			expected: true,
		},
		{
			filename: "requirements.txt",
			expected: true,
		},
		{
			filename: "requirements-test.txt",
			expected: true,
		},
		{
			filename: "pom.xml",
			expected: true,
		},
		{
			filename: "go.mod",
			expected: false,
		},
		{
			filename: "go.sum",
			expected: false,
		},
		{
			filename: "main.go",
			expected: false,
		},
		{
			filename: "test.txt",
			expected: false,
		},
	}

	for _, tt := range tests {
		got := osv.ForeignManifest(tt.filename)
		if got != tt.expected {
			t.Errorf("ForeignManifest(%q) = %v, want %v", tt.filename, got, tt.expected)
		}
	}
}

func TestOverrideContent(t *testing.T) {
	o := osv.Override{
		Dir:       "vendor/example.com/foo",
		Manifest:  "package-lock.json",
		Ecosystem: "npm",
		What:      "the dev dependencies",
	}

	content := osv.OverrideContent(o)
	if !strings.Contains(content, `ecosystem = "npm"`) {
		t.Errorf("expected ecosystem = npm, got: %s", content)
	}
	if !strings.Contains(content, "JavaScript") {
		t.Errorf("expected JavaScript language in reason, got: %s", content)
	}
}
