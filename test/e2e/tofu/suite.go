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

// Package tofu drives the OpenTofu provider against a real cluster.
//
// The other cluster suite walks the phases from inside the process. This one does not call
// cargoship at all: it runs the `tofu` binary over the module in example/terragrunt, which
// loads the provider out of a filesystem mirror, which starts the same phases in a subprocess
// the test does not control. That is the whole point -- everything between a practitioner's
// configuration and the phases is what has no coverage otherwise: the schema decoding, the
// map-keyed host set, the computed attributes a plan has to be able to promise, state, and a
// destroy.
package tofu

import (
	"os"
	"strconv"
	"sync"

	"github.com/colonel-byte/cargoship/test"
	blcluster "github.com/k0sproject/bootloose/pkg/cluster"
	"github.com/k0sproject/bootloose/pkg/config"
)

// The fleet is three machines on one image: one controller and two workers.
//
// One image rather than three, because nothing here routes on the OS family -- that is what the
// cluster suite covers, phase by phase, and repeating it would cost a second and third engine
// install for assertions already held elsewhere. Three machines rather than one, because two of
// the three things this suite is for need more than a single node: a worker has to be removable
// while the cluster keeps running, and the controller that drives the removal has to be a
// different machine from the one being removed.
const (
	bootImage = "ghcr.io/colonel-byte/bootloose/ubuntu-26:latest"
	bootKC    = "tkc%d"
	bootKW    = "tkw%d"

	// controllers and workers are how many of each the configuration below provisions. The
	// assertions read them rather than counting nodes by hand, so the fleet can grow by one
	// machine without a second place to edit.
	controllers = 1
	workers     = 2
	nodeCount   = controllers + workers
)

// providerVersion is the version the mirror serves and the module's `>=0.1.0` constraint
// resolves to. OpenTofu refuses 0.0.0 -- it is reserved for a provider that is not published --
// so the development loop builds 0.1.0 and so does this.
const providerVersion = "0.1.0"

// enginePackage is the definition the suite packages and installs, and the engine version it
// ships. It is k3s for the same reason the cluster suite's full walk is: a single-controller
// k3s cluster has a SQLite datastore and no etcd quorum to time out under the CPU contention of
// three nested node containers.
var enginePackage = struct { //nolint:gochecknoglobals
	path    string
	version string
}{
	path:    "example/k3s-flannel/v1_36/v1.36.4-k3s1",
	version: "1.36.4-k3s1",
}

// phaseTimeoutEnvVar is how long a phase waits on a host, as a Go duration. It is the `timeout`
// the configuration hands the provider.
//
// The default is the module's own, and the reason to lower it is diagnosis: a phase that is going
// to fail waits out the whole timeout first, so a run investigating a failure spends twenty
// minutes arriving at it. Six is enough for an engine that is going to start to have started.
const phaseTimeoutEnvVar = "CARGOSHIP_E2E_TOFU_PHASE_TIMEOUT"

func phaseTimeout() string {
	if value := os.Getenv(phaseTimeoutEnvVar); value != "" {
		return value
	}
	return "20m"
}

// keepClusterEnvVar leaves the machines running when the run failed, instead of deleting them on
// the way out. It is the same variable the cluster suite reads, and for the same reason: what a
// failed engine install leaves behind lives in the node containers' journals.
const keepClusterEnvVar = "CARGOSHIP_E2E_KEEP_CLUSTER"

func keepCluster() bool {
	on, err := strconv.ParseBool(os.Getenv(keepClusterEnvVar))
	return err == nil && on
}

var (
	e2e     test.CargoE2ETest //nolint:gochecknoglobals
	rootDir string            //nolint:gochecknoglobals

	// The machines are provisioned on first use rather than in TestMain, so a run that skips
	// for a missing `tofu` starts no container and needs no Docker.
	clusterOnce    sync.Once          //nolint:gochecknoglobals
	testCluster    *blcluster.Cluster //nolint:gochecknoglobals
	testClusterErr error              //nolint:gochecknoglobals

	// kubeconfigPath is where the suite writes the credentials the provider exported, and what
	// KUBECONFIG names for the whole run. TestMain owns it because the steps hand the cluster
	// to each other.
	kubeconfigPath string //nolint:gochecknoglobals
)

// fleet is the bootloose configuration this suite provisions.
//
// Every machine is privileged and mounts a volume over the engine's data directory, for the two
// reasons the cluster suite's main_test.go sets out at length: the engine loads modules and runs
// its own containerd, and overlayfs cannot be stacked on itself, so the snapshotter needs a
// directory that is not part of the container's root filesystem.
var fleet = config.Config{ //nolint:gochecknoglobals
	Cluster: config.Cluster{
		Name:       "cargoship-e2e-tofu",
		PrivateKey: "cluster-key-tofu",
	},
	Machines: []config.MachineReplicas{
		{Count: controllers, Spec: machine(bootKC)},
		{Count: workers, Spec: machine(bootKW)},
	},
}

func machine(name string) *config.Machine {
	return &config.Machine{
		Name:       name,
		Image:      bootImage,
		Privileged: true,
		PortMappings: []config.PortMapping{
			{ContainerPort: 22},
		},
		Volumes: []config.Volume{
			{Type: "volume", Destination: "/var/lib/rancher"},
		},
	}
}
