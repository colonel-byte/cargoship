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

// joinFedoraWorkers is how many kwf* machines exist once the join walk has added one, and
// joinHostname is the machine it adds. The join walk is the only walk that starts from a
// machine the install never saw, so a Fedora worker is what it adds: the new node then has to
// come through the SELinux, fapolicyd, firewalld and dnf branches on its own rather than
// inheriting anything the apply walk already proved on the nodes beside it.
const joinFedoraWorkers = 4

var joinHostname = fmt.Sprintf(bootKWF, joinFedoraWorkers-1) //nolint:gochecknoglobals

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

	// The join walk's machine is provisioned the same way, on first use, so that a run that
	// never reaches the join walk starts one fewer container.
	joinOnce sync.Once //nolint:gochecknoglobals
	joinErr  error     //nolint:gochecknoglobals

	// fullClusterConfigPath is the inventory holding every machine, including the upload-only
	// Alpine node. The phase harness is built from it, because the upload phases are meant to
	// see that node. e2e.ClusterConfigPath points at the cluster-only inventory instead: the
	// whole-action steps run prepare, apply and reset, which would try to install the engine
	// on a host that cannot run it.
	fullClusterConfigPath string //nolint:gochecknoglobals

	// kubeconfigPath is the file KUBECONFIG points at for the whole run. TestMain owns it
	// rather than a suite, because the suites hand the cluster to each other: the apply walk
	// writes it, the upgrade walk rewrites it, and the reset walk asserts it survives a
	// teardown that can no longer reach a controller.
	kubeconfigPath string //nolint:gochecknoglobals
)

// TestClusterPhases runs the four walks in the only order they work in. The apply walk
// installs the distro on the shared bootloose cluster, the join walk adds a machine to it, the
// upgrade walk moves that cluster to a newer package, and the reset walk takes the distro back
// off. They share one cluster and one kubeconfig, so they are subtests of one parent rather
// than four top-level tests whose order would depend on declaration order.
//
// The join walk runs before the upgrade rather than after it so that the upgrade has to carry
// the node that joined late as well as the nodes the install bootstrapped.
func TestClusterPhases(t *testing.T) {
	if !t.Run("apply", func(t *testing.T) { suite.Run(t, new(ApplyPhaseSuite)) }) {
		t.Log("apply failed: the join, upgrade and reset walks all need the cluster it installs")
		return
	}

	// The later walks run whatever the walk before them did. A half-joined or half-upgraded
	// cluster is still a cluster reset has to be able to tear down, and TestMain deletes the
	// containers either way.
	t.Run("join", func(t *testing.T) { suite.Run(t, new(JoinPhaseSuite)) })
	t.Run("upgrade", func(t *testing.T) { suite.Run(t, new(UpgradePhaseSuite)) })
	t.Run("reset", func(t *testing.T) { suite.Run(t, new(ResetSuite)) })
}

func TestMain(m *testing.M) {
	var err error
	// This suite calls the cargoship packages directly rather than shelling out, so it needs
	// the chdir into the repo root and the architecture, but no binary under build/.
	e2e, rootDir, err = test.BootstrapInProcess()
	if err != nil {
		log.Fatal(err)
	}

	kubeDir, err := os.MkdirTemp("", "cargoship-e2e-kube")
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

// requireJoinMachine adds the machine the join walk joins to the cluster, and rewrites both
// inventories so that every later walk sees it. It is the same bootloose config with one more
// Fedora worker in it: CreateMachine skips a container that already exists, so the call
// creates kwf3 and leaves the ten machines the apply walk installed running.
//
// The new cluster object replaces testCluster. bootloose finds a cluster's machines by walking
// the config it was built from rather than by asking Docker, so the object built from the
// ten-machine config would leave the eleventh container behind on shutdown.
func requireJoinMachine(t *testing.T) {
	t.Helper()
	requireCluster(t)

	joinOnce.Do(func() {
		cluster, err := setup(withJoinMachine(mixedOS))
		if err != nil {
			joinErr = err
			return
		}
		testCluster = cluster
		joinErr = writeInventories(cluster)
	})
	require.NoError(t, joinErr)
}

// withJoinMachine returns cfg with one more Fedora worker in it. It raises the count on the
// template the other Fedora workers come from rather than adding a second template, because
// bootloose names a machine by formatting its template's name with the replica index: a
// second template would start counting from zero again and collide with kwf0.
func withJoinMachine(cfg config.Config) config.Config {
	machines := make([]config.MachineReplicas, len(cfg.Machines))
	copy(machines, cfg.Machines)
	for i := range machines {
		if machines[i].Spec.Name == bootKWF {
			machines[i].Count = joinFedoraWorkers
		}
	}
	cfg.Machines = machines
	return cfg
}

// writeInventories renders the live bootloose machines into the two inventory files the walks
// read: the full one the phase harness is built from, and the engine-only one the CLI-driven
// steps are given. The join walk calls it a second time, once its machine is up, so that both
// files name the node it added.
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
// that it can run again over machines an earlier call already handled -- which is what the
// join walk does when it grows the cluster.
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
