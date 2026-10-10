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

// Package test provides e2e tests for cargoship
package noncluster

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCargoshipVerifyCommand exercises the `verify` command, which checks a package signature
// and does nothing else. The cases that matter are the ones the other commands cannot
// express: an unsigned package is a failure here rather than a skipped check, and there
// is no --verify mode to relax it.
//
// Signing keys come from cosignKeyPair, so the "unrelated key" case gets genuinely
// unrelated material and nothing is committed.
func TestCargoshipVerifyCommand(t *testing.T) {
	t.Run("verifies a signed package against its public key", func(t *testing.T) {
		privPath, pubPath := cosignKeyPair(t)
		signedDir := t.TempDir()

		_, _, err := e2e.Cargoship(t, "sign", minimalPackage(t),
			"--signing-key", privPath, "--signing-key-pass", cosignKeyPassword, "-o", signedDir)
		require.NoError(t, err)

		_, _, err = e2e.Cargoship(t, "verify", requireSinglePackage(t, signedDir), "-k", pubPath)
		require.NoError(t, err)
	})

	t.Run("an unrelated key fails", func(t *testing.T) {
		privPath, _ := cosignKeyPair(t)
		_, otherPubPath := cosignKeyPair(t)
		signedDir := t.TempDir()

		_, _, err := e2e.Cargoship(t, "sign", minimalPackage(t),
			"--signing-key", privPath, "--signing-key-pass", cosignKeyPassword, "-o", signedDir)
		require.NoError(t, err)

		_, _, err = e2e.Cargoship(t, "verify", requireSinglePackage(t, signedDir), "-k", otherPubPath)
		require.Error(t, err)
	})

	t.Run("an unsigned package fails", func(t *testing.T) {
		_, pubPath := cosignKeyPair(t)

		_, stderr, err := e2e.Cargoship(t, "verify", minimalPackage(t), "-k", pubPath)
		require.Error(t, err, "verify must not pass a package that carries no signature")
		require.Contains(t, stderr, "not signed")
	})

	t.Run("a missing package errors", func(t *testing.T) {
		_, pubPath := cosignKeyPair(t)

		_, _, err := e2e.Cargoship(t, "verify", filepath.Join(t.TempDir(), "nope.tar.zst"), "-k", pubPath)
		require.Error(t, err)
	})

	t.Run("no package source errors", func(t *testing.T) {
		_, _, err := e2e.Cargoship(t, "verify")
		require.Error(t, err)
	})

	t.Run("a key and a keyless identity together error", func(t *testing.T) {
		_, pubPath := cosignKeyPair(t)

		_, _, err := e2e.Cargoship(t, "verify", minimalPackage(t),
			"-k", pubPath, "--certificate-identity", "signer@example.com")
		require.Error(t, err)
	})
}
