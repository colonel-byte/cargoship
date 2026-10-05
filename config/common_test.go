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

package config

import (
	"path/filepath"
	"testing"
)

func TestGetAbsHomePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "tilde slash prefix",
			path: "~/.age/cargoship.key",
			want: filepath.Join(home, ".age", "cargoship.key"),
		},
		{
			name: "bare tilde",
			path: "~",
			want: home,
		},
		{
			name: "absolute path untouched",
			path: "/etc/cargoship/recipients.txt",
			want: "/etc/cargoship/recipients.txt",
		},
		{
			name: "relative path untouched",
			path: "./key.txt",
			want: "./key.txt",
		},
		{
			name: "empty path untouched",
			path: "",
			want: "",
		},
		{
			// A shell would resolve another user's home directory here. This cannot, so the
			// path is left as written rather than joined onto the running user's home.
			name: "another user's home untouched",
			path: "~operator/.age/cargoship.key",
			want: "~operator/.age/cargoship.key",
		},
		{
			// A cosign key provider, which `signing_key` accepts in place of a file path.
			name: "key provider URI untouched",
			path: "awskms:///alias/cargoship",
			want: "awskms:///alias/cargoship",
		},
		{
			name: "tilde inside the path untouched",
			path: "/etc/cargoship/~backup/key.txt",
			want: "/etc/cargoship/~backup/key.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetAbsHomePath(tt.path)
			if err != nil {
				t.Fatalf("GetAbsHomePath(%q) error = %v", tt.path, err)
			}
			if got != tt.want {
				t.Errorf("GetAbsHomePath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
