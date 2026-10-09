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

package tofu

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/test"
	blcluster "github.com/k0sproject/bootloose/pkg/cluster"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	var err error
	// Nothing here runs the CLI binary, so the in-process bootstrap is enough: it chdirs into
	// the repo root, which every relative path in this suite is written against.
	e2e, rootDir, err = test.BootstrapInProcess()
	if err != nil {
		log.Fatal(err)
	}

	kubeDir, err := os.MkdirTemp("", "cargoship-e2e-tofu-kube")
	if err != nil {
		log.Fatal(err)
	}
	kubeconfigPath = filepath.Join(kubeDir, "config")
	if err := os.Setenv("KUBECONFIG", kubeconfigPath); err != nil {
		log.Fatal(err)
	}

	code := m.Run()

	if err := os.RemoveAll(kubeDir); err != nil {
		log.Print(err)
	}
	if testCluster != nil && (code == 0 || !keepCluster()) {
		if err := testCluster.Delete(); err != nil {
			os.Exit(1)
		}
	}
	os.Exit(code)
}

// requireTofu skips the run when the machine cannot drive OpenTofu at all.
//
// Both halves are skips rather than failures, and they are deliberately not the same shape of
// problem. A missing `tofu` is a machine that was never set up for this suite. A missing mirror
// is a step that was not run, which is why the message names it: `mage test:endToEndTofu` builds
// the provider into the mirror before it runs the tests, so a run through mage has both.
func requireTofu(t *testing.T) string {
	t.Helper()

	// -short has to be safe to run anywhere, and this walk is four real applies over three
	// containers. It is also what keeps a `go test -short ./...` from colliding with a full run
	// of this suite: both would claim the same machine names.
	if testing.Short() {
		t.Skip("the provider walk needs a bootloose cluster, a real distro package and a tofu binary")
	}

	if _, err := exec.LookPath(tofuBinary()); err != nil {
		t.Skipf("%s is not on PATH: this suite drives the provider through the real OpenTofu CLI", tofuBinary())
	}

	mirror := filepath.Join(rootDir, providerMirrorDir)
	binary := filepath.Join(mirror, providerMirrorPath, providerVersion,
		hostPlatform(), providerBinaryName(providerVersion))
	if _, err := os.Stat(binary); err != nil {
		t.Skipf("the provider is not in the local mirror at %s: build it with `mage release:tofuProviderDev %s`, "+
			"or run the whole suite with `mage test:endToEndTofu`", binary, providerVersion)
	}
	return mirror
}

// requireCluster provisions the machines on first use.
func requireCluster(t *testing.T) {
	t.Helper()

	clusterOnce.Do(func() {
		// The kernel comes first: it is the one prerequisite that has nothing to do with the
		// machines, and finding it missing after provisioning them means three containers
		// started for a run that cannot install anything.
		if testClusterErr = requireKernelModules(); testClusterErr != nil {
			return
		}
		if testClusterErr = requireContainerNetworking(); testClusterErr != nil {
			return
		}
		testCluster, testClusterErr = blcluster.New(fleet)
		if testClusterErr != nil {
			return
		}
		if testClusterErr = testCluster.Create(); testClusterErr != nil {
			return
		}
		if testClusterErr = detachHostsFile(testCluster); testClusterErr != nil {
			return
		}
		// bootloose regenerates each container's SSH host key on every Create, and the
		// provider's phases dial those machines from inside the provider process.
		testClusterErr = os.Setenv("SSH_KNOWN_HOSTS", "")
	})
	require.NoError(t, testClusterErr)
}
