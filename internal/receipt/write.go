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

package receipt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// fileMode is what a receipt is created with.
//
// A receipt names every host in the fleet and carries whatever the phases recorded about them, so
// it is readable by its owner and nobody else. The 0640-owned-by-root form an audit standard
// would ask for is not reachable from a CLI that does not run as root, and pretending otherwise
// would leave a file that is neither.
const fileMode os.FileMode = 0o600

// dirMode is what a missing parent directory is created with, for the same reason.
const dirMode os.FileMode = 0o700

// Write serializes the receipt to path, creating the parent directory if it does not exist.
//
// The file is truncated and its mode reset on every write. Both matter on a path an operator
// reuses between runs: O_TRUNC alone would leave the tail of a longer previous receipt behind,
// and the create mode does not apply to a file that already exists, so a receipt written into a
// world-readable file left by something else would stay world-readable.
func Write(path string, r Receipt) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, dirMode); err != nil {
			return fmt.Errorf("unable to create the receipt directory %s: %w", dir, err)
		}
	}

	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("unable to encode the receipt: %w", err)
	}
	b = append(b, '\n')

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fileMode)
	if err != nil {
		return fmt.Errorf("unable to create the receipt %s: %w", path, err)
	}

	// The close error is joined rather than dropped: on a write-back filesystem it is where a
	// full disk surfaces, and a receipt that was reported as written and is not there is worse
	// than one that was reported as failed.
	err = writeReceipt(f, b, path)
	if cerr := f.Close(); cerr != nil && err == nil {
		err = fmt.Errorf("unable to close the receipt %s: %w", path, cerr)
	}
	return err
}

// writeReceipt sets the permissions and writes the body. It is split out so Write can close the
// file exactly once, on every path.
func writeReceipt(f *os.File, b []byte, path string) error {
	// Not fatal on its own, but it means the permissions are not what the receipt claims, which
	// is worth failing on for a file that exists to be evidence.
	if err := f.Chmod(fileMode); err != nil {
		return fmt.Errorf("unable to set permissions on the receipt %s: %w", path, err)
	}
	if _, err := f.Write(b); err != nil {
		return fmt.Errorf("unable to write the receipt %s: %w", path, err)
	}
	return nil
}

// Read loads a receipt from path.
func Read(path string) (Receipt, error) {
	var r Receipt
	b, err := os.ReadFile(path)
	if err != nil {
		return r, fmt.Errorf("unable to read the receipt %s: %w", path, err)
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, fmt.Errorf("unable to parse the receipt %s: %w", path, err)
	}
	return r, nil
}

// redactedValue replaces the value of a flag known to carry a secret.
const redactedValue = "<redacted>"

// credentialFlags are the flags whose values are secrets.
//
// It is deliberately a short list, and worth saying why rather than leaving the next reader to
// wonder whether something was missed. None of the key material the lifecycle commands accept is
// passed as a value: --vault-password-file, --age-identity-file and --age-recipients-file are
// paths, and an age recipient is a public key. A path names where a secret lives without carrying
// it, which is the property that makes the command line reproducible and safe to record, and the
// Ansible module already depends on exactly that -- see internal/ansiblemod.Detail.Command.
//
// So this table is defensive rather than load-bearing today. It is here because the receipt
// records a command line verbatim, and the first flag that does take an inline secret must not
// need anyone to remember this file exists.
var credentialFlags = map[string]bool{
	"registry-password":      true,
	"registry-pull-password": true,
	"registry-push-password": true,
	"registry-secret":        true,
	"git-pull-password":      true,
	"git-push-password":      true,
	"password":               true,
	"token":                  true,
}

// redactArgs replaces the value of any credential flag in an argument vector.
//
// Both spellings are handled: --flag value, where the value is the next argument, and
// --flag=value, where it is part of the same one.
func redactArgs(argv []string) []string {
	if len(argv) == 0 {
		return nil
	}

	out := make([]string, 0, len(argv))
	redactNext := false
	for _, arg := range argv {
		if redactNext {
			out = append(out, redactedValue)
			redactNext = false
			continue
		}

		name, _, joined := strings.Cut(arg, "=")
		if !strings.HasPrefix(name, "-") {
			out = append(out, arg)
			continue
		}
		if !credentialFlags[strings.TrimLeft(name, "-")] {
			out = append(out, arg)
			continue
		}

		if joined {
			// Keep the flag name so the shape of the command is still readable; only the
			// value goes.
			out = append(out, name+"="+redactedValue)
			continue
		}
		out = append(out, arg)
		redactNext = true
	}
	return out
}
