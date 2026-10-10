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

	apicluster "github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
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
	// joinGroup, when set, is the group withJoinMachine appends rather than growing an
	// existing one.
	//
	// A fleet this suite provisions grows a group it already has, because it can create
	// another machine from the same template. An externally supplied fleet cannot: the host
	// the join walk needs is one the operator provisioned and named, so it is held here and
	// left out of machines until withJoinMachine puts it back.
	joinGroup *machineSpec
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
	// role is the group's role, when the fleet knows it independently of the name. Empty
	// means the name carries it, which is how the container and VM fleets work: their
	// hostnames are chosen so that a prefix claims a role. An externally supplied inventory
	// has no such naming, so it sets this instead and the prefixes are never consulted.
	role string
	// count is how many replicas of this group to provision.
	count int
}

// names is the hostnames one replica group produces.
//
// A template carrying no formatting verb is a literal name, which is what an externally
// supplied inventory has: those hostnames are whatever the operator's fleet calls them and are
// not a pattern this suite gets to choose. Such a group holds one host.
func (m machineSpec) names() []string {
	if !strings.Contains(m.nameTemplate, "%") {
		return []string{m.nameTemplate}
	}
	out := make([]string, 0, m.count)
	for i := 0; i < m.count; i++ {
		out = append(out, fmt.Sprintf(m.nameTemplate, i))
	}
	return out
}

// roleOf is the role a group's hosts take.
func (m machineSpec) roleOf() string {
	if m.role != "" {
		return m.role
	}
	switch prefix := strings.TrimSuffix(m.nameTemplate, "%d"); {
	case strings.HasPrefix(prefix, uploadOnlyPrefix):
		return apicluster.RoleWorker
	case strings.HasPrefix(prefix, controllerPrefix):
		return apicluster.RoleController
	case strings.HasPrefix(prefix, workerPrefix):
		return apicluster.RoleWorker
	default:
		return ""
	}
}

// isUploadOnlyGroup reports whether a group's hosts are in the inventory only to receive
// uploads. Only a named group can be: an external inventory has no such host.
func (m machineSpec) isUploadOnlyGroup() bool {
	return strings.HasPrefix(strings.TrimSuffix(m.nameTemplate, "%d"), uploadOnlyPrefix)
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

	if f.joinGroup != nil {
		f.machines = append(machines, *f.joinGroup)
		return f
	}
	if i := f.lastWorkerGroup(); i >= 0 {
		machines[i].count++
	}
	f.machines = machines
	return f
}

// joinHostname is the machine withJoinMachine adds.
func (f fleetSpec) joinHostname() string {
	if f.joinGroup != nil {
		return f.joinGroup.nameTemplate
	}
	i := f.lastWorkerGroup()
	if i < 0 {
		return ""
	}
	m := f.machines[i]
	if !strings.Contains(m.nameTemplate, "%") {
		return m.nameTemplate
	}
	return fmt.Sprintf(m.nameTemplate, m.count)
}

// lastWorkerGroup is the index of the final replica group holding engine-running workers, or
// -1 when the fleet has none. Upload-only groups are skipped: the join walk joins a node to
// the cluster, which is the one thing those machines cannot do.
func (f fleetSpec) lastWorkerGroup() int {
	last := -1
	for i, m := range f.machines {
		if m.isUploadOnlyGroup() {
			continue
		}
		if m.roleOf() == apicluster.RoleWorker {
			last = i
		}
	}
	return last
}

// hostnames is every machine name the spec produces, in provisioning order.
func (f fleetSpec) hostnames() []string {
	var names []string
	for _, m := range f.machines {
		names = append(names, m.names()...)
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

// countsFor reads those counts off a fleet spec, per replica group rather than per hostname, so
// that a group which states its role is counted by that rather than by what its names look
// like. A group that states neither a role nor a recognised prefix is counted in the inventory
// total and nowhere else.
func countsFor(f fleetSpec) clusterCounts {
	var c clusterCounts
	for _, m := range f.machines {
		n := len(m.names())
		c.inventory += n
		switch {
		case m.isUploadOnlyGroup():
			c.uploadOnly += n
		case m.roleOf() == apicluster.RoleController:
			c.controllers += n
		case m.roleOf() == apicluster.RoleWorker:
			c.workers += n
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
