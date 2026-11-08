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

package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// readByteStrictTarget mirrors the shape ReadByteStrict is used with -- a nested section, so that a
// bad key below the top level is covered as well as one at it.
type readByteStrictTarget struct {
	ZarfCache string `yaml:"zarf_cache"`
	Distro    struct {
		Concurrency int `yaml:"concurrency"`
	} `yaml:"distro"`
}

// TestReadByteStrictRejectsUnknownKeys pins the strictness itself. ReadByteStrict previously took
// the address of its own interface parameter, which handed goccy a **T: it filled the interface
// rather than the struct behind it, so the destination kept its zero value and Strict() had no
// fields to compare a key against. Every misspelled key was accepted silently.
func TestReadByteStrictRejectsUnknownKeys(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name:    "unknown key at the top level",
			yaml:    "log_lvel: debug\n",
			wantErr: `unknown field "log_lvel"`,
		},
		{
			name:    "unknown key in a nested section",
			yaml:    "distro:\n  concurency: 300\n",
			wantErr: `unknown field "concurency"`,
		},
		{
			name:    "malformed yaml",
			yaml:    "not yaml: [unterminated\n",
			wantErr: "failed to unmarshal YAML",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got readByteStrictTarget
			err := ReadByteStrict([]byte(tt.yaml), &got)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestReadByteStrictDecodesIntoDestination is the other half of the same bug, and the half that is
// easy to miss: with the double pointer a *valid* document also left the destination at its zero
// value. Asserting the decoded values guards against a future refactor reintroducing that, which a
// test checking only the error would not catch.
func TestReadByteStrictDecodesIntoDestination(t *testing.T) {
	tests := []struct {
		name            string
		yaml            string
		wantZarfCache   string
		wantConcurrency int
	}{
		{
			name:            "top-level and nested values both land",
			yaml:            "zarf_cache: /tmp/cache\ndistro:\n  concurrency: 7\n",
			wantZarfCache:   "/tmp/cache",
			wantConcurrency: 7,
		},
		{
			name:            "empty document leaves the zero value",
			yaml:            "",
			wantZarfCache:   "",
			wantConcurrency: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got readByteStrictTarget
			require.NoError(t, ReadByteStrict([]byte(tt.yaml), &got))
			require.Equal(t, tt.wantZarfCache, got.ZarfCache)
			require.Equal(t, tt.wantConcurrency, got.Distro.Concurrency)
		})
	}
}
