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

package cluster

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/colonel-byte/cargoship/src/test"
	blcluster "github.com/k0sproject/bootloose/pkg/cluster"
	"github.com/k0sproject/bootloose/pkg/config"
	"github.com/stretchr/testify/require"
)

// The cluster deliberately runs three OS families. Several apply phases route on the family --
// the RPM, APT and BIN upload phases, SELinux, fapolicyd, the firewall backend -- and a
// single-family cluster leaves every branch it does not match untested, with the gate
// assertions passing vacuously. Fedora brings the dnf/rpm and firewalld paths the Debian image
// never exercises. Alpine is neither family, which is the only way the BIN upload phase gets
// tested as the path it is rather than as a fallback; it does not run the engine, see
// uploadOnlyPrefix in cluster_inventory.go.
const (
	bootUbuntu = "ghcr.io/colonel-byte/bootloose/ubuntu-26:latest"
	bootFedora = "ghcr.io/colonel-byte/bootloose/fedora-44:latest"
	bootAlpine = "ghcr.io/colonel-byte/bootloose/alpine-3.23:latest"
)

// Machine name templates. The inventory maps a machine to a role by prefix, "kc" for
// controller and "kw" for worker, so the trailing letter marks the image without needing a
// second lookup: kc0, kc1, kcf0, kw0, kw1, kw2, kwf0, kwf1, kwf2, kwa0.
const (
	bootKC  = "kc%d"
	bootKCF = "kcf%d"
	bootKW  = "kw%d"
	bootKWF = "kwf%d"
	// bootKWA is built from uploadOnlyPrefix so that the name the machines get and the prefix
	// the inventory tests for cannot drift apart.
	bootKWA = uploadOnlyPrefix + "%d"
)

var (
	e2e   test.CargoE2ETest //nolint:gochecknoglobals
	ports = []config.PortMapping{
		{
			ContainerPort: 22,
		},
	}
	// mixedOS provisions ten machines: a nine-node cluster of three controllers and six
	// workers with each role split across the Ubuntu and Fedora images, plus one Alpine
	// machine that receives uploads and never joins. Nine cluster nodes is enough that the
	// worker concurrency batching in the initialize and upgrade phases runs more than one
	// batch at the WorkerConcurrent the suite sets, which a six-node cluster did not.
	mixedOS = config.Config{
		Cluster: config.Cluster{
			Name:       "cargoship-e2e",
			PrivateKey: "cluster-key",
		},
		Machines: []config.MachineReplicas{
			{
				Count: 2,
				Spec: &config.Machine{
					Name:         bootKC,
					Image:        bootUbuntu,
					PortMappings: ports,
				},
			},
			{
				Count: 1,
				Spec: &config.Machine{
					Name:         bootKCF,
					Image:        bootFedora,
					PortMappings: ports,
				},
			},
			{
				Count: 3,
				Spec: &config.Machine{
					Name:         bootKW,
					Image:        bootUbuntu,
					PortMappings: ports,
				},
			},
			{
				Count: 3,
				Spec: &config.Machine{
					Name:         bootKWF,
					Image:        bootFedora,
					PortMappings: ports,
				},
			},
			{
				Count: 1,
				Spec: &config.Machine{
					Name:         bootKWA,
					Image:        bootAlpine,
					PortMappings: ports,
				},
			},
		},
	}
)

var (
	// rootDir is the repo root, which TestMain chdirs into before running any test.
	rootDir string //nolint:gochecknoglobals

	// The bootloose cluster is provisioned lazily by requireCluster rather than in TestMain,
	// so a filtered run (-short, or -run against a test that never calls it) still starts no
	// container and needs no Docker.
	clusterOnce    sync.Once          //nolint:gochecknoglobals
	testCluster    *blcluster.Cluster //nolint:gochecknoglobals
	testClusterErr error              //nolint:gochecknoglobals

	// fullClusterConfigPath is the inventory holding every machine, including the upload-only
	// Alpine node. The phase harness is built from it, because the upload phases are meant to
	// see that node. e2e.ClusterConfigPath points at the cluster-only inventory instead: the
	// whole-action steps run prepare, apply and reset, which would try to install the engine
	// on a host that cannot run it.
	fullClusterConfigPath string //nolint:gochecknoglobals
)

func TestMain(m *testing.M) {
	var err error
	// This suite calls the cargoship packages directly rather than shelling out, so it needs
	// the chdir into the repo root and the architecture, but no binary under build/.
	e2e, rootDir, err = test.BootstrapInProcess()
	if err != nil {
		log.Fatal(err)
	}

	code := m.Run()

	if testCluster != nil {
		if err := shutdown(testCluster); err != nil {
			os.Exit(1)
		}
	}
	os.Exit(code)
}

// requireCluster provisions the shared bootloose cluster on first use and points e2e at the
// generated inventory.
func requireCluster(t *testing.T) {
	t.Helper()

	clusterOnce.Do(func() {
		testCluster, testClusterErr = setup(mixedOS)
		if testClusterErr != nil {
			return
		}

		keyPath, err := filepath.Abs(mixedOS.Cluster.PrivateKey)
		if err != nil {
			testClusterErr = err
			return
		}
		inv, err := renderClusterInventory(testCluster, keyPath)
		if err != nil {
			testClusterErr = err
			return
		}
		invDir := filepath.Join(rootDir, "src/test/e2e/cluster")

		fullPath := filepath.Join(invDir, "generated-cluster-full.yaml")
		if err := writeClusterInventory(inv, fullPath); err != nil {
			testClusterErr = err
			return
		}
		fullClusterConfigPath = fullPath

		invPath := filepath.Join(invDir, "generated-cluster.yaml")
		if err := writeClusterInventory(engineHosts(inv), invPath); err != nil {
			testClusterErr = err
			return
		}
		e2e.ClusterConfigPath = invPath

		// bootloose regenerates each container's SSH host key on every Create(), so ignore
		// host key checking for the cargoship subprocesses this test binary spawns.
		testClusterErr = os.Setenv("SSH_KNOWN_HOSTS", "")
	})
	require.NoError(t, testClusterErr)
}

func setup(config config.Config) (*blcluster.Cluster, error) {
	cluster, err := blcluster.New(config)
	if err != nil {
		return nil, err
	}
	return cluster, cluster.Create()
}

func shutdown(cluster *blcluster.Cluster) error {
	return cluster.Delete()
}
