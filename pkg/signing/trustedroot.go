// Copyright 2021 zarf authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from zarf:
// https://github.com/zarf-dev/zarf
//
// Modifications Copyright 2026 colonel-byte.
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

package signing

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
)

// embeddedTrustedRoot is the Sigstore TrustedRoot JSON shipped with the binary.
// Nothing updates this automatically; it is a documented manual bump.
// See docs/dev/signing.md for how to refresh it and what to check afterwards.
//
//go:embed embedded_trusted_root.json
var embeddedTrustedRoot []byte

// writeEmbeddedTrustedRoot stages the embedded TrustedRoot JSON to a tempfile so
// cosign verification commands, which accept trusted roots as file paths, can consume it.
// Caller must invoke cleanup when done; cleanup returns the os.Remove error.
// Will use the os default temporary directory if empty string is supplied
func writeEmbeddedTrustedRoot(tmpDir string) (string, func() error, error) {
	// os.CreateTemp will use os default if tmpdir is an empty string
	if tmpDir != "" {
		if err := os.MkdirAll(tmpDir, 0700); err != nil {
			return "", func() error { return nil }, fmt.Errorf("creating temp directory: %w", err)
		}
	}
	f, err := os.CreateTemp(tmpDir, "cargoship-trusted-root-*.json")
	if err != nil {
		return "", func() error { return nil }, fmt.Errorf("creating tempfile: %w", err)
	}
	cleanup := func() error { return os.Remove(f.Name()) }

	if _, writeErr := f.Write(embeddedTrustedRoot); writeErr != nil {
		closeErr := f.Close()
		removeErr := cleanup()
		return "", func() error { return nil },
			fmt.Errorf("writing embedded trusted root: %w", errors.Join(writeErr, closeErr, removeErr))
	}
	if closeErr := f.Close(); closeErr != nil {
		removeErr := cleanup()
		return "", func() error { return nil },
			fmt.Errorf("closing embedded trusted root tempfile: %w", errors.Join(closeErr, removeErr))
	}
	return f.Name(), cleanup, nil
}
