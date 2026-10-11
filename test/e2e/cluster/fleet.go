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
	"strings"

	"github.com/k0sproject/bootloose/pkg/config"
)

// fleetSpec describes the nodes a walk needs: how many of each role, under which names, on
// which image. It says nothing about what provisions them.
//
// The suite used to describe its nodes as a bootloose config.Config and read everything it
// needed back off that -- the role counts, the join walk's extra machine. That works for
// exactly one backend. Everything the walks actually care about is here instead, and a backend
// is a function from this to whatever it needs: bootlooseConfig is the one for containers.
const (
	// controllerPrefix and workerPrefix are the hostname prefixes the inventory maps to roles.
	// uploadOnlyPrefix begins with workerPrefix, so anything testing these has to test it
	// first; see its own comment in cluster_inventory.go.
	controllerPrefix = "kc"
	workerPrefix     = "kw"
)

type fleetSpec struct {
	// name is the cluster name, which bootloose labels its containers with.
	name string
	// machines are the replica groups, in the order they are provisioned.
	machines []machineSpec
}

// machineSpec is one replica group: a name template, a count, and the image to use.
type machineSpec struct {
	// nameTemplate is formatted with the replica index to name each machine, so that the
	// inventory's prefix-to-role mapping claims them. See uploadOnlyPrefix.
	nameTemplate string
	// image is the container image. A backend that does not provision containers ignores it.
	image string
	// osID is the os-release ID the machines in this group report, which phase 09 asserts the
	// detected OS against. It lives on the group rather than in a prefix-to-ID table beside
	// the test because it is a property of the image, and a second backend running a
	// different OS under the same names would silently invalidate such a table.
	osID string
	// count is how many replicas of this group to provision.
	count int
}

// bootlooseConfig renders the spec as the config bootloose provisions containers from.
func (f fleetSpec) bootlooseConfig() config.Config {
	machines := make([]config.MachineReplicas, 0, len(f.machines))
	for _, m := range f.machines {
		machines = append(machines, config.MachineReplicas{
			Count: m.count,
			Spec:  stageMachine(m.nameTemplate, m.image),
		})
	}
	return config.Config{
		Cluster: config.Cluster{
			Name:       f.name,
			PrivateKey: "cluster-key",
		},
		Machines: machines,
	}
}

// withJoinMachine returns the spec with one more worker in it, in the last worker group.
//
// It raises a count rather than adding a group, because a machine is named by formatting its
// group's template with the replica index: a second group would start counting from zero again
// and collide with a machine that already exists.
//
// The last worker group rather than a named one, because which group that is carries the point
// of the join walk and differs per fleet. On the container fleets it is the Fedora group, so
// the node that joins has to come through the SELinux, fapolicyd, firewalld and dnf branches
// on its own rather than inheriting what the apply walk proved on the Ubuntu nodes beside it.
// On a single-distribution fleet there is nothing to contrast with and the last group is
// simply the only one.
func (f fleetSpec) withJoinMachine() fleetSpec {
	machines := make([]machineSpec, len(f.machines))
	copy(machines, f.machines)

	if i := f.lastWorkerGroup(); i >= 0 {
		machines[i].count++
	}
	f.machines = machines
	return f
}

// joinHostname is the machine withJoinMachine adds.
func (f fleetSpec) joinHostname() string {
	i := f.lastWorkerGroup()
	if i < 0 {
		return ""
	}
	return fmt.Sprintf(f.machines[i].nameTemplate, f.machines[i].count)
}

// lastWorkerGroup is the index of the final replica group holding engine-running workers, or
// -1 when the fleet has none. Upload-only groups are skipped: the join walk joins a node to
// the cluster, which is the one thing those machines cannot do.
func (f fleetSpec) lastWorkerGroup() int {
	last := -1
	for i, m := range f.machines {
		prefix := strings.TrimSuffix(m.nameTemplate, "%d")
		if strings.HasPrefix(prefix, uploadOnlyPrefix) {
			continue
		}
		if strings.HasPrefix(prefix, workerPrefix) {
			last = i
		}
	}
	return last
}

// hostnames is every machine name the spec produces, in provisioning order.
func (f fleetSpec) hostnames() []string {
	var names []string
	for _, m := range f.machines {
		for i := 0; i < m.count; i++ {
			names = append(names, fmt.Sprintf(m.nameTemplate, i))
		}
	}
	return names
}

// clusterCounts is how many hosts of each kind a spec produces, split the way the suite
// asserts on them. Deriving these from the spec rather than writing them down twice is what
// lets the same assertions hold against either inventory.
type clusterCounts struct {
	inventory   int
	uploadOnly  int
	controllers int
	// workers counts the hosts that run the engine, so it leaves out the upload-only machines
	// even though the inventory gives them the worker role.
	workers int
}

// countsFor reads those counts off a fleet spec, by the same name prefixes the inventory maps
// to roles. The upload-only prefix is tested first because it begins with the worker prefix.
func countsFor(f fleetSpec) clusterCounts {
	var c clusterCounts
	for _, name := range f.hostnames() {
		c.inventory++
		switch {
		case strings.HasPrefix(name, uploadOnlyPrefix):
			c.uploadOnly++
		case strings.HasPrefix(name, controllerPrefix):
			c.controllers++
		case strings.HasPrefix(name, workerPrefix):
			c.workers++
		}
	}
	return c
}

// osIDFor returns the os-release ID the machines behind a hostname report, or an empty string
// for a name no replica group in the fleet produces.
//
// The longest matching prefix wins, because the templates nest: "kw" is a prefix of "kwf" and
// of "kwa", so a shortest-match walk would call every Fedora and Alpine worker Ubuntu.
func (f fleetSpec) osIDFor(hostname string) string {
	id, longest := "", 0
	for _, m := range f.machines {
		prefix := strings.TrimSuffix(m.nameTemplate, "%d")
		if strings.HasPrefix(hostname, prefix) && len(prefix) > longest {
			id, longest = m.osID, len(prefix)
		}
	}
	return id
}

// osIDs returns the distinct os-release IDs the fleet provisions.
//
// It reads the fleet rather than a fixed list because not every fleet uses every replica group
// -- k3sOS, for one, has no Alpine host -- and a family this run never provisions must not
// count toward what detectOS requires the cluster to have.
func (f fleetSpec) osIDs() map[string]struct{} {
	ids := make(map[string]struct{}, len(f.machines))
	for _, m := range f.machines {
		if m.osID != "" {
			ids[m.osID] = struct{}{}
		}
	}
	return ids
}
