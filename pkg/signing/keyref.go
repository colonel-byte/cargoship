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

package signing

import (
	"fmt"
	"os"
	"strings"
)

// envKeyPrefix is cosign's scheme for a key held in an environment variable.
const envKeyPrefix = "env://"

// kmsKeyPrefixes are the cosign key-provider schemes that resolve a key by reaching a
// cloud endpoint. They are named so that a config written against them fails with the
// reason rather than with a missing-file error for a path nobody meant as a path.
var kmsKeyPrefixes = []string{"awskms://", "gcpkms://", "azurekms://", "hashivault://", "openbao://"}

// readKeyMaterial resolves a cosign key reference to PEM bytes.
//
// A bare path is a file. `env://NAME` reads the variable, which is how a CI pipeline
// keeps a private key off the build host's disk without needing a cloud - see
// docs/guides/signing-packages.md. The cloud KMS schemes are rejected by name: they
// were removed with the keyless work, because none of them can be reached from an
// air-gapped management node, and "no such file or directory" is a poor way to say so.
func readKeyMaterial(ref string) ([]byte, error) {
	if ref == "" {
		return nil, fmt.Errorf("no key reference given")
	}

	if name, ok := strings.CutPrefix(ref, envKeyPrefix); ok {
		if name == "" {
			return nil, fmt.Errorf("%s names no environment variable", ref)
		}
		value, ok := os.LookupEnv(name)
		if !ok {
			return nil, fmt.Errorf("the environment variable %s named by %s is not set", name, ref)
		}
		if value == "" {
			return nil, fmt.Errorf("the environment variable %s named by %s is empty", name, ref)
		}
		return []byte(value), nil
	}

	for _, prefix := range kmsKeyPrefixes {
		if strings.HasPrefix(ref, prefix) {
			return nil, fmt.Errorf(
				"%s key references are no longer supported: a cloud KMS cannot be reached from an air-gapped management node, so cargoship takes a local keypair or env:// only - see https://github.com/colonel-byte/cargoship/issues/848",
				strings.TrimSuffix(prefix, "://"),
			)
		}
	}

	return os.ReadFile(ref) //nolint:gosec // the operator names the key on the command line
}
