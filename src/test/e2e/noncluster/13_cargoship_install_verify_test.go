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

package noncluster

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// clusterFixture is a one-host inventory that is never connected to. Every case in this
// file fails while the package is loaded, which happens before any host is contacted.
const clusterFixture = "src/test/e2e/noncluster/testdata/cluster.yaml"

// TestCargoshipInstallVerify covers signature verification on the install command group.
// These commands load a package through initManager, so they must honor --verify and the
// verification material flags the same way the package group does.
//
// Only refusal paths are exercised: a package that passes verification would let the
// command continue on to the hosts in the inventory, which this suite does not have.
func TestCargoshipInstallVerify(t *testing.T) {
	for _, command := range []string{"apply", "prepare", "engine-config-sync"} {
		t.Run(command+" refuses an unsigned package with --verify=always", func(t *testing.T) {
			_, stderr, err := e2e.Cargoship(t, command, minimalPackage(t),
				"--config", clusterFixture, "--confirm", "--verify=always")
			require.Error(t, err)
			require.Contains(t, stderr, "package is not signed")
		})

		t.Run(command+" refuses a signed package when the key does not match", func(t *testing.T) {
			signedPkg, _ := signedPackage(t)
			_, otherPubPath := cosignKeyPair(t)

			_, stderr, err := e2e.Cargoship(t, command, signedPkg,
				"--config", clusterFixture, "--confirm", "--key", otherPubPath)
			require.Error(t, err)
			require.Contains(t, stderr, "signature verification failed")
		})
	}

	t.Run("apply reads the public key from the environment", func(t *testing.T) {
		signedPkg, _ := signedPackage(t)
		_, otherPubPath := cosignKeyPair(t)

		t.Setenv("DISTRO_DISTRO_PUBLIC_KEY", otherPubPath)
		_, stderr, err := e2e.Cargoship(t, "apply", signedPkg,
			"--config", clusterFixture, "--confirm")
		require.Error(t, err)
		require.Contains(t, stderr, "signature verification failed")
	})
}
