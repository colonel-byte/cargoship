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
	"strconv"
	"strings"
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
	// engineData mounts a Docker volume over the engine's data directory on every machine.
	//
	// The engine runs its own containerd, whose default snapshotter is overlayfs, and overlayfs
	// cannot be stacked on itself: given an upperdir that is already on an overlay mount the
	// kernel refuses with EINVAL. Whether that happens depends on the storage driver of the
	// Docker running the nodes. With overlay2 -- the default, and what a GitHub hosted runner
	// uses -- a container's root filesystem is an overlay mount, so containerd's snapshotter
	// lands on one and the engine never gets past "server is not ready".
	//
	// An anonymous volume is not part of that root filesystem. Docker backs it with a directory
	// on whatever filesystem holds /var/lib/docker, so the snapshotter gets a plain one and
	// mounts normally. This is the same arrangement kind's node image makes with its VOLUME
	// declaration, for the same reason.
	//
	// The volume is anonymous rather than named on purpose: a named volume would carry one
	// run's engine state into the next, and every walk here assumes it starts from nodes with
	// no engine on them. The cost is that the volumes outlive `docker rm -f`, which does not
	// remove them -- stopBootlooseContainers in magefiles/test-e2e-cluster.go passes -v so a
	// local run does not accumulate them.
	engineData = []config.Volume{
		{
			Type:        "volume",
			Destination: "/var/lib/rancher",
		},
	}
	// mixedOS provisions ten machines, and is what a full walk runs against -- a stage-only
	// run uses stageOS instead. It is a nine-node cluster of three controllers and six
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
					Volumes:      engineData,
				},
			},
			{
				Count: 1,
				Spec: &config.Machine{
					Name:         bootKCF,
					Image:        bootFedora,
					Privileged:   true,
					PortMappings: ports,
					Volumes:      engineData,
				},
			},
			{
				Count: 3,
				Spec: &config.Machine{
					Name:         bootKW,
					Image:        bootUbuntu,
					Privileged:   true,
					PortMappings: ports,
					Volumes:      engineData,
				},
			},
			{
				Count: 3,
				Spec: &config.Machine{
					Name:         bootKWF,
					Image:        bootFedora,
					Privileged:   true,
					PortMappings: ports,
					Volumes:      engineData,
				},
			},
			{
				Count: 1,
				Spec: &config.Machine{
					Name:         bootKWA,
					Image:        bootAlpine,
					Privileged:   true,
					PortMappings: ports,
					Volumes:      engineData,
				},
			},
		},
	}

	// stageOS is the inventory a stage-only run provisions instead: one machine per family per
	// role, and the upload-only Alpine node. It is half the containers of mixedOS and half the
	// uploads, which is what makes the stage job cheap enough to be worth running on its own.
	//
	// Five is the floor rather than three, because the upload phases route on family and role
	// together -- APTUploadFiles.Prepare filters p.control and p.workers by both -- and the file
	// list is picked per role. With one machine per family each family has to take a single
	// role, and the other role's host set for that family is empty, so half the cells the
	// upload phases branch on go unvisited. That is the axis the bug in phase/55's host
	// claiming lived on, so it is the one cell a smaller inventory must not lose.
	//
	// Nothing else the staging phases do is sensitive to how many machines there are. The
	// upload batching that mixedOS's ten machines justify is bounded by applyConcurrency, which
	// is 300, so every host already goes in one batch; and the WorkerConcurrent batching is
	// only reached in the initialize and upgrade phases, which a stage-only run skips.
	stageOS = config.Config{
		Cluster: config.Cluster{
			Name:       "cargoship-e2e",
			PrivateKey: "cluster-key",
		},
		Machines: []config.MachineReplicas{
			{Count: 1, Spec: stageMachine(bootKC, bootUbuntu)},
			{Count: 1, Spec: stageMachine(bootKCF, bootFedora)},
			{Count: 1, Spec: stageMachine(bootKW, bootUbuntu)},
			{Count: 1, Spec: stageMachine(bootKWF, bootFedora)},
			{Count: 1, Spec: stageMachine(bootKWA, bootAlpine)},
		},
	}
)

// stageMachine is one machine of stageOS. Every machine there differs only in its name and its
// image, and the rest is what mixedOS gives its machines and for the same reasons: see the
// comments on mixedOS for the privilege, and on engineData for the volume.
func stageMachine(name, image string) *config.Machine {
	return &config.Machine{
		Name:         name,
		Image:        image,
		Privileged:   true,
		PortMappings: ports,
		Volumes:      engineData,
	}
}

// clusterConfig is the inventory this run provisions: the smaller one when the run was asked for
// the staging phases only, and the full one otherwise.
func clusterConfig() config.Config {
	if stageOnly() {
		return stageOS
	}
	return mixedOS
}

// clusterCounts is how many hosts of each kind a bootloose config produces, split the way the
// suite asserts on them. Deriving these from the config rather than writing them down twice is
// what lets the same assertions hold against either inventory.
type clusterCounts struct {
	inventory   int
	uploadOnly  int
	controllers int
	// workers counts the hosts that run the engine, so it leaves out the upload-only machines
	// even though renderClusterInventory gives them the worker role.
	workers int
}

// countsFor reads those counts off a bootloose config, by the same name prefixes
// renderClusterInventory maps to roles. The upload-only prefix is tested first because it
// begins with the worker prefix.
func countsFor(cfg config.Config) clusterCounts {
	var c clusterCounts
	for _, m := range cfg.Machines {
		c.inventory += m.Count
		switch {
		case strings.HasPrefix(m.Spec.Name, uploadOnlyPrefix):
			c.uploadOnly += m.Count
		case strings.HasPrefix(m.Spec.Name, "kc"):
			c.controllers += m.Count
		case strings.HasPrefix(m.Spec.Name, "kw"):
			c.workers += m.Count
		}
	}
	return c
}

// stageOnlyEnvVar stops the run at the boundary phase/60 draws. The phases up to and including
// it stage files and render the engine config onto the nodes without starting anything; every
// phase after it installs, starts or queries the engine. It is off by default, so a local run
// walks everything.
//
// The two halves are worth separating because they cost different amounts and depend on
// different things. The staging half finishes in about three minutes and asks nothing of the
// machine beyond Docker. The engine half brings up a nine-node rke2 cluster, which wants more
// CPU than a hosted runner has, and needs a container runtime nested inside the node
// containers -- see engineData for what that costs. Turning this on is how a runner that
// cannot give the engine half what it needs still covers the phases that do not need it.
const stageOnlyEnvVar = "CARGOSHIP_E2E_STAGE_ONLY"

// stageOnly reports whether the run was asked to stop before the engine phases.
func stageOnly() bool {
	on, err := strconv.ParseBool(os.Getenv(stageOnlyEnvVar))
	return err == nil && on
}

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
	// writes it and the join walk runs against the cluster it names.
	kubeconfigPath string //nolint:gochecknoglobals
)

// TestClusterPhases runs the two walks in the only order they work in. The apply walk installs
// the distro on the shared bootloose cluster and the join walk adds a machine to what it
// installed. They share one cluster and one kubeconfig, so they are subtests of one parent
// rather than two top-level tests whose order would depend on declaration order.
func TestClusterPhases(t *testing.T) {
	if !t.Run("apply", func(t *testing.T) { suite.Run(t, new(ApplyPhaseSuite)) }) {
		t.Log("apply failed: the join walk needs the cluster it installs")
		return
	}

	t.Run("join", func(t *testing.T) { suite.Run(t, new(JoinPhaseSuite)) })
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
		testCluster, testClusterErr = setup(clusterConfig())
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
	keyPath, err := filepath.Abs(clusterConfig().Cluster.PrivateKey)
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
