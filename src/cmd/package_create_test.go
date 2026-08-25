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

package cmd

import (
	"reflect"
	"testing"

	"github.com/colonel-byte/cargoship/src/pkg/images"
)

func TestParseRegistryOverridesSortsLongestPrefixFirst(t *testing.T) {
	got, err := parseRegistryOverrides([]string{
		"docker.io=docker.example.com",
		"docker.io/library=library.example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []images.RegistryOverride{
		{Source: "docker.io/library", Override: "library.example.com"},
		{Source: "docker.io", Override: "docker.example.com"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseRegistryOverrides() = %+v, want %+v", got, want)
	}
}

func TestParseRegistryOverridesEmpty(t *testing.T) {
	got, err := parseRegistryOverrides(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("parseRegistryOverrides(nil) = %+v, want empty", got)
	}
}

func TestParseRegistryOverridesErrors(t *testing.T) {
	tests := []struct {
		name  string
		input []string
	}{
		{"missing equals", []string{"docker.io"}},
		{"missing source", []string{"=docker.example.com"}},
		{"missing value", []string{"docker.io="}},
		{"duplicate source", []string{"docker.io=a.example.com", "docker.io=b.example.com"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseRegistryOverrides(tt.input); err == nil {
				t.Fatalf("parseRegistryOverrides(%v) = nil error, want error", tt.input)
			}
		})
	}
}
