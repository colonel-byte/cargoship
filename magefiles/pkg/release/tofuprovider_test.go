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

package release

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateVersion(t *testing.T) {
	cases := []struct {
		name    string
		version string
		wantErr bool
	}{
		{
			name:    "plain version",
			version: "0.1.0",
			wantErr: false,
		},
		{
			name:    "build metadata",
			version: "0.1.0+build.1",
			wantErr: false,
		},
		{
			name:    "leading v",
			version: "v0.1.0",
			wantErr: true,
		},
		{
			name:    "empty",
			version: "",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateVersion(tc.version)
			if tc.wantErr && err == nil {
				t.Fatalf("validateVersion(%q) = nil, want an error", tc.version)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateVersion(%q) = %v, want nil", tc.version, err)
			}
		})
	}
}

// TestZipBinaryHoldsOneEntryAtTheRoot pins the package layout OpenTofu unpacks. A nested directory
// or a second file is a package it will not read, and neither would fail anywhere else: the push
// succeeds, the index is well formed, and the provider only fails to install.
func TestZipBinaryHoldsOneEntryAtTheRoot(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "nested", "terraform-provider-cargoship_v0.1.0")
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, []byte("not really a binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(dir, "provider.zip")
	if err := zipBinary(archive, binaryPath, "terraform-provider-cargoship_v0.1.0"); err != nil {
		t.Fatalf("zipBinary: %v", err)
	}

	r, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatalf("opening the archive: %v", err)
	}
	defer r.Close() //nolint:errcheck // nothing is written to it

	if len(r.File) != 1 {
		t.Fatalf("the archive holds %d entries, want 1", len(r.File))
	}
	if got := r.File[0].Name; got != "terraform-provider-cargoship_v0.1.0" {
		t.Errorf("entry is named %q, want the binary at the root of the archive", got)
	}
	// The fixed modification time is what makes two builds of the same commit produce the same
	// bytes, which is what an operator comparing a digest against a mirror relies on.
	if got := r.File[0].Modified.UTC(); !got.Equal(zipEpoch) {
		t.Errorf("entry carries %s, want the fixed epoch %s", got, zipEpoch)
	}
}

// TestProviderPlatformsAreUnique guards the index assembly: two entries for one platform would
// push the second manifest over the first in the layout, and the index would name a tag that no
// longer holds what it was built from.
func TestProviderPlatformsAreUnique(t *testing.T) {
	seen := make(map[string]bool, len(providerPlatforms))
	for _, platform := range providerPlatforms {
		key := platform[0] + "/" + platform[1]
		if seen[key] {
			t.Errorf("%s is listed twice", key)
		}
		seen[key] = true
	}
	if len(providerPlatforms) == 0 {
		t.Fatal("no platforms are published, so the index would hold nothing")
	}
}
