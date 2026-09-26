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

package layout

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizePermissionsDirectoriesAndFiles(t *testing.T) {
	root := t.TempDir()

	dirs := []string{
		"images",
		"images/blobs/sha256",
		"os",
		"os/0",
	}
	for _, d := range dirs {
		path := filepath.Join(root, d)
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatalf("creating dir %s: %v", path, err)
		}
	}

	makeFile := func(rel string, mode os.FileMode) {
		path := filepath.Join(root, rel)
		if err := os.WriteFile(path, []byte("x"), mode); err != nil {
			t.Fatalf("creating file %s: %v", path, err)
		}
	}

	// Non-executable regular files.
	makeFile("checksums.txt", 0o600)
	makeFile("images/index.json", 0o400)
	makeFile("os/0/rpm", 0o600)

	// Executable file.
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o700); err != nil {
		t.Fatalf("creating dir bin: %v", err)
	}
	makeFile("bin/tool", 0o700)

	// Symlink pointing outside the layout. os.Chmod follows symlinks, so if
	// normalizePermissions didn't skip it, this would silently chmod the link's
	// target instead of leaving it alone.
	targetPath := filepath.Join(t.TempDir(), "outside-target")
	if err := os.WriteFile(targetPath, []byte("x"), 0o600); err != nil {
		t.Fatalf("creating symlink target: %v", err)
	}
	linkPath := filepath.Join(root, "link-to-outside")
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Fatalf("creating symlink: %v", err)
	}

	d := &DistroLayout{dirPath: root}
	if err := d.normalizePermissions(); err != nil {
		t.Fatalf("normalizePermissions error: %v", err)
	}

	checkMode := func(rel string, want os.FileMode) {
		path := filepath.Join(root, rel)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("unexpected mode for %s: got %v, want %v", path, got, want)
		}
	}

	// Directories normalized to 0755.
	for _, dRel := range dirs {
		checkMode(dRel, 0o755)
	}
	checkMode("bin", 0o755)

	// Regular files normalized to 0644.
	checkMode("checksums.txt", 0o644)
	checkMode("images/index.json", 0o644)
	checkMode("os/0/rpm", 0o644)

	// Executable file normalized to 0755.
	checkMode("bin/tool", 0o755)

	// Symlink target's mode must be untouched -- proves the symlink itself was
	// skipped rather than followed and chmod'd.
	targetInfo, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("stat symlink target %s: %v", targetPath, err)
	}
	if got := targetInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("symlink target mode changed: got %v, want %v", got, os.FileMode(0o600))
	}
}
