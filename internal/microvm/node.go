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

package microvm

import (
	"fmt"
	"hash/fnv"

	apicluster "github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
)

const (
	// defaultFleet is the fleet brought up when no name is given.
	defaultFleet = "dev"

	// controllerPrefix and workerPrefix match the names the bootloose cluster uses, so that
	// the same prefix-to-role mapping, and the same role counting, read both fleets. See
	// test/e2e/cluster/cluster_inventory.go.
	controllerPrefix = "kc"
	workerPrefix     = "kw"

	// privateDomain is the search domain nodes get, which is what makes a host's
	// LongHostname differ from its Hostname the way a real fleet's does.
	privateDomain = "cargoship.test"

	// privatePrefixLen is the mask on the private segment.
	privatePrefixLen = 24

	// privateHostOffset is added to a node's index to get its last address octet, keeping
	// the fleet clear of the .1 a reader expects to be a gateway.
	privateHostOffset = 10

	// maxNodes caps a fleet. The limit is not qemu's: one byte of the MAC and one octet of
	// the private address are the node index, and a developer's machine runs out of memory
	// long before either runs out of room. A low cap turns a typo into an error instead of
	// into a fleet that exhausts the host.
	maxNodes = 32
)

// Spec describes a fleet to bring up. The zero value is not usable; Normalize fills in the
// defaults and rejects what cannot be defaulted.
type Spec struct {
	// Fleet names the fleet, which scopes its run directory, its addresses and its ports.
	Fleet string
	// Controllers and Workers are how many of each role to create.
	Controllers int
	Workers     int
	// MemoryMiB and CPUs are per node.
	MemoryMiB int
	CPUs      int
	// DiskSize is the virtual size of each node's overlay, in qemu-img syntax.
	DiskSize string
	// Distro selects which engine API ports are forwarded from the leader to the host, so
	// that kube-config works from the management node. Either k3s or rke2.
	Distro string
	// RunRoot is the directory fleet state is written under. Empty means build/microvm.
	RunRoot string
}

// Node is one virtual machine in a fleet. Every address, port and MAC on it is derived from
// the fleet name and the node index rather than assigned at bring-up, so that a second
// command -- Down, List, SSH -- recomputes exactly what Up used without reading any state.
type Node struct {
	// Name is the hostname, and the directory name under the fleet's run directory.
	Name string
	// Role is apicluster.RoleController or apicluster.RoleWorker.
	Role string
	// Index is the node's 1-based position in the fleet, across both roles.
	Index int
	// SSHPort is the loopback port on the host forwarded to this node's sshd.
	SSHPort int
	// PrivateAddress is this node's address on the fleet's layer 2 segment. It is written
	// into the inventory explicitly rather than discovered, because the slirp interface is
	// the one holding the default route: fact gathering would pick that, and every phase
	// that distributes peer addresses would then distribute addresses no peer can reach.
	PrivateAddress string
	// MgmtMAC and LANMAC address the two interfaces. cloud-init matches on these.
	MgmtMAC string
	LANMAC  string
	// HostFwd holds any additional loopback forwards, beyond SSH, for this node.
	HostFwd []PortForward
}

// PortForward is one loopback-to-guest TCP forward on the management interface.
type PortForward struct {
	HostPort  int
	GuestPort int
}

// Normalize fills in the defaults a Spec leaves empty and reports what it cannot default.
func (s Spec) Normalize() (Spec, error) {
	if s.Fleet == "" {
		s.Fleet = defaultFleet
	}
	if s.MemoryMiB == 0 {
		s.MemoryMiB = 4096
	}
	if s.CPUs == 0 {
		s.CPUs = 2
	}
	if s.DiskSize == "" {
		s.DiskSize = "20G"
	}
	if s.Distro == "" {
		s.Distro = "k3s"
	}
	if s.Controllers < 1 {
		return Spec{}, fmt.Errorf("a fleet needs at least one controller, got %d", s.Controllers)
	}
	if s.Workers < 0 {
		return Spec{}, fmt.Errorf("a fleet cannot have %d workers", s.Workers)
	}
	if total := s.Controllers + s.Workers; total > maxNodes {
		return Spec{}, fmt.Errorf("a fleet of %d nodes exceeds the %d node limit", total, maxNodes)
	}
	if s.Distro != "k3s" && s.Distro != "rke2" {
		return Spec{}, fmt.Errorf("distro must be k3s or rke2, got %q", s.Distro)
	}
	return s, nil
}

// Nodes derives the fleet's nodes from the spec. Controllers come first so that index 1 is
// always the leader, which is what the inventory's load balancer points at.
func (s Spec) Nodes() ([]Node, error) {
	spec, err := s.Normalize()
	if err != nil {
		return nil, err
	}
	id := fleetID(spec.Fleet)

	nodes := make([]Node, 0, spec.Controllers+spec.Workers)
	index := 0
	add := func(prefix, role string, n int) {
		for i := 0; i < n; i++ {
			index++
			node := Node{
				Name:           fmt.Sprintf("%s%d", prefix, i),
				Role:           role,
				Index:          index,
				SSHPort:        sshPortFor(id, index),
				PrivateAddress: privateAddressFor(id, index),
				MgmtMAC:        fmt.Sprintf("52:54:00:%02x:00:%02x", id, index),
				LANMAC:         fmt.Sprintf("52:54:00:%02x:01:%02x", id, index),
			}
			if role == apicluster.RoleController && i == 0 {
				node.HostFwd = leaderForwards(id, spec.Distro)
			}
			nodes = append(nodes, node)
		}
	}
	add(controllerPrefix, apicluster.RoleController, spec.Controllers)
	add(workerPrefix, apicluster.RoleWorker, spec.Workers)
	return nodes, nil
}

// leaderForwards are the engine ports forwarded from the leader to the host, so that
// kube-config and kubectl work from the management node rather than only from inside the
// fleet. rke2 serves its join endpoint on a second port; k3s multiplexes onto 6443.
func leaderForwards(id int, distro string) []PortForward {
	fwd := []PortForward{
		{
			HostPort:  apiPortFor(id, 6443),
			GuestPort: 6443,
		},
	}
	if distro == "rke2" {
		fwd = append(fwd, PortForward{
			HostPort:  apiPortFor(id, 9345),
			GuestPort: 9345,
		})
	}
	return fwd
}

// fleetID is a small stable number derived from the fleet name. Two fleets with different
// names then get different ports, different MACs and different private segments, so they can
// run side by side; two checkouts using the same name deliberately collide, and the pidfile
// is what reports that.
func fleetID(fleet string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(fleet))
	return int(h.Sum32() % 200)
}

// sshPortFor is the loopback port forwarded to a node's sshd.
func sshPortFor(id, index int) int {
	return 20000 + id*40 + index
}

// apiPortFor is the loopback port forwarded to one of the leader's engine ports. The guest
// port is folded in so that 6443 and 9345 cannot land on the same host port.
func apiPortFor(id, guestPort int) int {
	return 28000 + id*10 + guestPort%10
}

// privateAddressFor is a node's address on the fleet's own layer 2 segment.
func privateAddressFor(id, index int) string {
	return fmt.Sprintf("10.73.%d.%d", id, privateHostOffset+index)
}

// mcastFor is the multicast group and port carrying the fleet's layer 2 segment between its
// qemu processes.
func mcastFor(id int) string {
	return fmt.Sprintf("239.192.73.1:%d", 14000+id)
}
