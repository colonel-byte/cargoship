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

const (
	bootUbuntu = "ghcr.io/colonel-byte/bootloose/ubuntu-26:latest"
	bootKC     = "kc%d"
	bootKI     = "ki%d"
	bootKW     = "kw%d"
)

var (
	e2e   test.CargoE2ETest //nolint:gochecknoglobals
	ports = []config.PortMapping{
		{
			ContainerPort: 22,
		},
	}
	ubuntu = config.Config{
		Cluster: config.Cluster{
			Name:       "ubuntu",
			PrivateKey: "cluster-key",
		},
		Machines: []config.MachineReplicas{
			{
				Count: 3,
				Spec: &config.Machine{
					Name:         bootKC,
					Image:        bootUbuntu,
					PortMappings: ports,
				},
			},
			{
				Count: 3,
				Spec: &config.Machine{
					Name:         bootKI,
					Image:        bootUbuntu,
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
)

func TestMain(m *testing.M) {
	var err error
	e2e, rootDir, err = test.Bootstrap()
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
		testCluster, testClusterErr = setup(ubuntu)
		if testClusterErr != nil {
			return
		}

		keyPath, err := filepath.Abs(ubuntu.Cluster.PrivateKey)
		if err != nil {
			testClusterErr = err
			return
		}
		inv, err := renderClusterInventory(testCluster, keyPath)
		if err != nil {
			testClusterErr = err
			return
		}
		invPath := filepath.Join(rootDir, "src/test/e2e/cluster/generated-cluster.yaml")
		if err := writeClusterInventory(inv, invPath); err != nil {
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
