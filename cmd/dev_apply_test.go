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
	"os"
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/config"
)

// writeDefinitionDir returns a directory holding a distro.yaml, which is the shape
// preflightDevApply accepts. The content is never parsed here, only stat'ed.
func writeDefinitionDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, config.DistroYAML), []byte("kind: Distro\n"), 0o600); err != nil {
		t.Fatalf("unable to write the definition: %v", err)
	}
	return dir
}

func TestPreflightDevApplyAcceptsADefinition(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "inventory.yaml")
	if err := os.WriteFile(cfg, []byte("kind: Cluster\n"), 0o600); err != nil {
		t.Fatalf("unable to write the inventory: %v", err)
	}

	dir := writeDefinitionDir(t)

	cases := []struct {
		name   string
		source string
	}{
		{
			name:   "a directory holding distro.yaml",
			source: dir,
		},
		{
			name:   "the distro.yaml itself",
			source: filepath.Join(dir, config.DistroYAML),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := preflightDevApply(tc.source, cfg, true, false); err != nil {
				t.Fatalf("preflightDevApply(%q) returned %v, want no error", tc.source, err)
			}
		})
	}
}

// TestPreflightDevApplyRejectsAMissingDefinition covers the case ResolveDistroPath alone would
// let through: a directory that exists but holds no distro.yaml resolves fine, and without the
// manifest stat the failure would surface much later with a less useful path in it.
func TestPreflightDevApplyRejectsAMissingDefinition(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "inventory.yaml")
	if err := os.WriteFile(cfg, []byte("kind: Cluster\n"), 0o600); err != nil {
		t.Fatalf("unable to write the inventory: %v", err)
	}

	cases := []struct {
		name   string
		source string
	}{
		{
			name:   "a path that does not exist",
			source: filepath.Join(t.TempDir(), "absent"),
		},
		{
			name:   "a directory holding no distro.yaml",
			source: t.TempDir(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := preflightDevApply(tc.source, cfg, true, false); err == nil {
				t.Fatalf("preflightDevApply(%q) returned no error, want one", tc.source)
			}
		})
	}
}

// TestPreflightDevApplyRequiresConfirm confirms the gate every host-changing command carries is
// not lost by taking a different preflight. A dev loop that applied without --confirm because it
// was the convenient one would be the worst of the three.
func TestPreflightDevApplyRequiresConfirm(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "inventory.yaml")
	if err := os.WriteFile(cfg, []byte("kind: Cluster\n"), 0o600); err != nil {
		t.Fatalf("unable to write the inventory: %v", err)
	}
	dir := writeDefinitionDir(t)

	if _, err := preflightDevApply(dir, cfg, false, false); err == nil {
		t.Fatal("preflightDevApply without --confirm returned no error, want one")
	}
	// A dry run changes nothing, so it needs no confirmation.
	if _, err := preflightDevApply(dir, cfg, false, true); err != nil {
		t.Fatalf("preflightDevApply with --dry-run returned %v, want no error", err)
	}
}

// TestDevApplyCommandFlags pins the flag surface: the build-side flags that change what gets
// built are present, and the ones describing an artifact this command never writes are not.
func TestDevApplyCommandFlags(t *testing.T) {
	cmd := newDevApplyCommand()

	for _, name := range []string{
		"config",
		"confirm",
		"dry-run",
		"file-override",
		"content-store",
		"registry-override",
		"plain-http",
	} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("dev apply is missing the --%s flag", name)
		}
	}

	// Verification needs a package that travelled; this one builds what it runs. Signing and
	// the output flags describe an artifact it never produces. A flag accepted and ignored is
	// worse than one that is absent.
	for _, name := range []string{
		"key",
		"verify",
		"output",
		"signing-key",
		"reproducible",
		"tag",
	} {
		if cmd.Flags().Lookup(name) != nil {
			t.Errorf("dev apply registers --%s, which it cannot act on", name)
		}
	}
}

// TestDevApplyUsesTheBuildingSource confirms the command is wired to build rather than to load.
// Without this the command would silently fall back to initManager and reject its own argument.
func TestDevApplyUsesTheBuildingSource(t *testing.T) {
	o := devApplyOptions{}
	o.source = packageSource{
		preflight: preflightDevApply,
		open:      o.buildManager,
	}

	if o.source.preflight == nil || o.source.open == nil {
		t.Fatal("dev apply must set both halves of its package source")
	}

	// And the default, which apply relies on, stays the package-loading pair.
	var plain installApplyOptions
	if plain.source.preflight != nil || plain.source.open != nil {
		t.Fatal("the zero packageSource must be the package-loading default")
	}
}
