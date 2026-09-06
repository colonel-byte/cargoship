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
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/colonel-byte/cargoship/src/test"
	blcluster "github.com/k0sproject/bootloose/pkg/cluster"
	"github.com/k0sproject/bootloose/pkg/config"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
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
	//
	// Every machine is privileged, which is what lets the engine run in a container at all:
	// the engine mounts filesystems, loads kernel modules and runs its own containerd, none of
	// which an unprivileged container is allowed to do. It is also what lets the prepare phase
	// finish. That phase runs `sysctl --system`, which applies every file in the image's own
	// sysctl.d directories as well as the one cargoship writes, and the Fedora image ships
	// defaults for keys that are not network namespaced -- vm.max_map_count, kernel.pid_max,
	// fs.protected_symlinks. Unprivileged, /proc/sys is mounted read only, those keys are
	// refused, and sysctl exits 1 even though the file cargoship wrote applied cleanly.
	//
	// The cost is that the settings land on the kernel of whatever machine runs the tests,
	// because those keys are shared with the host. That is the same bargain every
	// engine-in-Docker test harness makes, and the values are the distro defaults the nodes
	// would set anyway.
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
					Privileged:   true,
					PortMappings: ports,
				},
			},
			{
				Count: 1,
				Spec: &config.Machine{
					Name:         bootKCF,
					Image:        bootFedora,
					Privileged:   true,
					PortMappings: ports,
				},
			},
			{
				Count: 3,
				Spec: &config.Machine{
					Name:         bootKW,
					Image:        bootUbuntu,
					Privileged:   true,
					PortMappings: ports,
				},
			},
			{
				Count: 3,
				Spec: &config.Machine{
					Name:         bootKWF,
					Image:        bootFedora,
					Privileged:   true,
					PortMappings: ports,
				},
			},
			{
				Count: 1,
				Spec: &config.Machine{
					Name:         bootKWA,
					Image:        bootAlpine,
					Privileged:   true,
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

// TestClusterPhases runs the apply walk against the shared bootloose cluster. It is a subtest
// rather than a top-level test because the walks that will run after it take the cluster this
// one leaves behind, and a subtest is what fixes the order they run in.
func TestClusterPhases(t *testing.T) {
	t.Run("apply", func(t *testing.T) { suite.Run(t, new(ApplyPhaseSuite)) })
}

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
		if testClusterErr = writeInventories(testCluster); testClusterErr != nil {
			return
		}

		// bootloose regenerates each container's SSH host key on every Create(), so ignore
		// host key checking for the cargoship subprocesses this test binary spawns.
		testClusterErr = os.Setenv("SSH_KNOWN_HOSTS", "")
	})
	require.NoError(t, testClusterErr)
}

// writeInventories renders the live bootloose machines into the two inventory files the walk
// reads: the full one the phase harness is built from, and the engine-only one the CLI-driven
// steps are given.
func writeInventories(c *blcluster.Cluster) error {
	keyPath, err := filepath.Abs(mixedOS.Cluster.PrivateKey)
	if err != nil {
		return err
	}
	inv, err := renderClusterInventory(c, keyPath)
	if err != nil {
		return err
	}
	invDir := filepath.Join(rootDir, "src/test/e2e/cluster")

	fullPath := filepath.Join(invDir, "generated-cluster-full.yaml")
	if err := writeClusterInventory(inv, fullPath); err != nil {
		return err
	}
	fullClusterConfigPath = fullPath

	invPath := filepath.Join(invDir, "generated-cluster.yaml")
	if err := writeClusterInventory(engineHosts(inv), invPath); err != nil {
		return err
	}
	e2e.ClusterConfigPath = invPath

	return nil
}

func setup(config config.Config) (*blcluster.Cluster, error) {
	cluster, err := blcluster.New(config)
	if err != nil {
		return nil, err
	}
	if err := cluster.Create(); err != nil {
		return nil, err
	}
	return cluster, detachHostsFile(cluster)
}

// detachHostsFileScript replaces the bind mount Docker puts over /etc/hosts with an ordinary
// file holding the same lines. It does nothing when /etc/hosts is already an ordinary file, so
// that it can run again over machines an earlier call already handled.
const detachHostsFileScript = `
if grep -q ' /etc/hosts ' /proc/mounts; then
	cp /etc/hosts /tmp/hosts.detach &&
	umount /etc/hosts &&
	cat /tmp/hosts.detach > /etc/hosts &&
	rm -f /tmp/hosts.detach
fi
`

// detachHostsFile runs that script on every machine in the cluster.
//
// Docker bind mounts a file it owns over /etc/hosts in every container, and a bind-mounted
// file cannot be replaced from inside: phase/25_modify_hosts_file.go writes a temp file and
// installs it over /etc/hosts, which is what a host expects and what fails here with EBUSY.
// The failure is the container, not the phase, and it is not even reported consistently --
// the Ubuntu image's install prints the error and exits 0, so the same broken write passes
// there and fails on Fedora and Alpine.
//
// Undoing the mount leaves the file it was covering, with the addresses Docker had already
// written into it, and the nodes can then rewrite it the way a real host would. What is given
// up is Docker's own upkeep of that file, which matters only if a machine's address changes
// while the cluster is up. Nothing in the suite restarts a machine, and the phase under test
// is the one that maintains those entries from then on.
func detachHostsFile(c *blcluster.Cluster) error {
	machines, err := c.Inspect(nil)
	if err != nil {
		return fmt.Errorf("failed to list the cluster's machines: %w", err)
	}
	for _, machine := range machines {
		name := machine.ContainerName()
		out, err := exec.Command("docker", "exec", name, "sh", "-c", detachHostsFileScript).CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to detach /etc/hosts on %s: %w: %s", name, err, out)
		}
	}
	return nil
}

func shutdown(cluster *blcluster.Cluster) error {
	return cluster.Delete()
}
