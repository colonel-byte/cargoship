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

package test

import (
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/src/test"
	"github.com/k0sproject/bootloose/pkg/cluster"
	"github.com/k0sproject/bootloose/pkg/config"
	zconfig "github.com/zarf-dev/zarf/src/config"
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

func TestMain(m *testing.M) {
	rootDir, err := filepath.Abs("../../../")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.Chdir(rootDir); err != nil {
		log.Fatal(err)
	}

	e2e.Arch = zconfig.GetArch()
	cargoBinPath, err := filepath.Abs(filepath.Join("build", test.GetCLIName()))
	if err != nil {
		log.Fatal(err)
	}
	e2e.CargoBinPath = cargoBinPath
	if _, err := os.Stat(e2e.CargoBinPath); err != nil {
		log.Fatalf("cargoship binary %s not found: %v", e2e.CargoBinPath, err)
	}
	cluster, err := setup(ubuntu)
	if err != nil {
		os.Exit(1)
	}

	keyPath, err := filepath.Abs(ubuntu.Cluster.PrivateKey)
	if err != nil {
		log.Fatal(err)
	}
	inv, err := renderClusterInventory(cluster, keyPath)
	if err != nil {
		log.Fatal(err)
	}
	invPath := filepath.Join(rootDir, "src/test/e2e/generated-cluster.yaml")
	if err := writeClusterInventory(inv, invPath); err != nil {
		log.Fatal(err)
	}
	e2e.ClusterConfigPath = invPath

	// bootloose regenerates each container's SSH host key on every Create(), so ignore
	// host key checking for the cargoship subprocesses this test binary spawns.
	if err := os.Setenv("SSH_KNOWN_HOSTS", ""); err != nil {
		log.Fatal(err)
	}

	code := m.Run()
	if err := shutdown(cluster); err != nil {
		os.Exit(1)
	}
	os.Exit(code)
}

func setup(config config.Config) (*cluster.Cluster, error) {
	cluster, err := cluster.New(config)
	if err != nil {
		return nil, err
	}
	return cluster, cluster.Create()
}

func shutdown(cluster *cluster.Cluster) error {
	return cluster.Delete()
}
