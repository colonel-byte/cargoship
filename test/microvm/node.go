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
	"errors"
	"fmt"
	"hash/fnv"

	apicluster "github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
)

const (
	// defaultFleet is the fleet brought up when no name is given.
	defaultFleet = "dev"

	// controllerPrefix and workerPrefix match the names the bootloose cluster uses, so that
	// the same prefix-to-role mapping, and the same role counting, read both fleets. See
	// test/e2e/cluster/cluster_inventory.go.
	controllerPrefix = "kc"
	workerPrefix     = "kw"

	// infraPrefix names the infra nodes. It is a prefix of its own rather than an extension
	// of workerPrefix, so the three groups partition the names and nothing matching on them
	// depends on test order or on matching longest.
	//
	// The consequence is that a mapping keyed only on kc and kw claims an infra node as
	// neither. Nothing here is such a mapping -- Inventory reads a node's Role and Profile
	// off the node rather than off its name -- but the container suite's
	// renderClusterInventory is, so wiring infra nodes into that fleet means teaching it this
	// prefix rather than relying on the name to carry the role.
	infraPrefix = "ki"

	// ubuntuControllerPrefix and ubuntuWorkerPrefix name the opt-in Ubuntu nodes. They are
	// prefixes of their own rather than extensions of kc and kw, so the five groups partition
	// the names and nothing matching on them depends on test order.
	ubuntuControllerPrefix = "uc"
	ubuntuWorkerPrefix     = "uw"

	// infraProfile is the profile infra nodes select, which pkg/phase/81_label_nodes.go turns
	// into a node-role.kubernetes.io/infra label and which a cluster config can hang a
	// per-profile concurrency off.
	infraProfile = "infra"

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
	// UbuntuControllers and UbuntuWorkers are how many nodes run Ubuntu instead of Rocky.
	//
	// Opt in, both defaulting to zero: a Rocky-only fleet is what the package was built for,
	// and it is what every existing caller gets without changing anything. Asking for Ubuntu
	// nodes is how the family-routed branches a single-distribution fleet cannot reach -- the
	// APT upload phase, the ufw backend, the Debian Configurer, and the SELinux and fapolicyd
	// gates correctly doing nothing -- get exercised against a real kernel.
	UbuntuControllers int
	UbuntuWorkers     int
	// Infra is how many infra nodes to create: workers carrying a profile of their own, so
	// that the node-role labelling and the per-profile concurrency are exercised against a
	// profile that is not simply the role's name.
	Infra int
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
	// Role is apicluster.RoleController or apicluster.RoleWorker. Infra nodes are workers:
	// the engine has no third role.
	Role string
	// Profile is the profile the node selects. It matches Role for controllers and plain
	// workers, and is infraProfile for infra nodes.
	Profile string
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
	// Image is the id of the guest this node runs, and OSID the os-release ID it reports.
	Image string
	OSID  string
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
	if s.Controllers < 0 || s.UbuntuControllers < 0 {
		return Spec{}, fmt.Errorf("a fleet cannot have %d Rocky and %d Ubuntu controllers", s.Controllers, s.UbuntuControllers)
	}
	if s.Controllers+s.UbuntuControllers < 1 {
		return Spec{}, errors.New("a fleet needs at least one controller")
	}
	if s.UbuntuWorkers < 0 {
		return Spec{}, fmt.Errorf("a fleet cannot have %d Ubuntu workers", s.UbuntuWorkers)
	}
	if s.Workers < 0 {
		return Spec{}, fmt.Errorf("a fleet cannot have %d workers", s.Workers)
	}
	if s.Infra < 0 {
		return Spec{}, fmt.Errorf("a fleet cannot have %d infra nodes", s.Infra)
	}
	if total := s.nodeCount(); total > maxNodes {
		return Spec{}, fmt.Errorf("a fleet of %d nodes exceeds the %d node limit", total, maxNodes)
	}
	switch s.Distro {
	case "k3s", "rke2", "upstream":
	default:
		return Spec{}, fmt.Errorf("distro must be k3s, rke2 or upstream, got %q", s.Distro)
	}
	return s, nil
}

// nodeCount is how many nodes the spec asks for, across every group.
func (s Spec) nodeCount() int {
	return s.Controllers + s.UbuntuControllers + s.Workers + s.UbuntuWorkers + s.Infra
}

// group is one set of like nodes: a name prefix, the role they join as, the profile they
// select, the image they run, and how many there are.
type group struct {
	prefix  string
	role    string
	profile string
	img     image
	count   int
}

// groups is the fleet's node sets, in the order they are created.
//
// Controllers come first so that index 1 is always the leader, which is what the inventory's
// load balancer points at -- and the Rocky controllers come before the Ubuntu ones, so a
// mixed fleet's leader is the guest that can run either engine. rke2 ships RPMs and links
// against glibc, so an Ubuntu node cannot run it at all; k3s installs from a binary and runs
// on both.
//
// Infra comes last so that adding infra nodes does not renumber the workers ahead of them,
// and a fleet asking for neither Ubuntu nodes nor infra is numbered exactly as it was before
// either existed.
func (s Spec) groups() []group {
	return []group{
		{
			prefix:  controllerPrefix,
			role:    apicluster.RoleController,
			profile: apicluster.RoleController,
			img:     rockyImage,
			count:   s.Controllers,
		},
		{
			prefix:  ubuntuControllerPrefix,
			role:    apicluster.RoleController,
			profile: apicluster.RoleController,
			img:     ubuntuImage,
			count:   s.UbuntuControllers,
		},
		{
			prefix:  workerPrefix,
			role:    apicluster.RoleWorker,
			profile: apicluster.RoleWorker,
			img:     rockyImage,
			count:   s.Workers,
		},
		{
			prefix:  ubuntuWorkerPrefix,
			role:    apicluster.RoleWorker,
			profile: apicluster.RoleWorker,
			img:     ubuntuImage,
			count:   s.UbuntuWorkers,
		},
		{
			prefix:  infraPrefix,
			role:    apicluster.RoleWorker,
			profile: infraProfile,
			img:     rockyImage,
			count:   s.Infra,
		},
	}
}

// Nodes derives the fleet's nodes from the spec.
func (s Spec) Nodes() ([]Node, error) {
	spec, err := s.Normalize()
	if err != nil {
		return nil, err
	}
	id := fleetID(spec.Fleet)

	nodes := make([]Node, 0, spec.nodeCount())
	index := 0
	leaderAssigned := false
	for _, g := range spec.groups() {
		for i := 0; i < g.count; i++ {
			index++
			node := Node{
				Name:           fmt.Sprintf("%s%d", g.prefix, i),
				Role:           g.role,
				Profile:        g.profile,
				Index:          index,
				SSHPort:        sshPortFor(id, index),
				PrivateAddress: privateAddressFor(id, index),
				MgmtMAC:        fmt.Sprintf("52:54:00:%02x:00:%02x", id, index),
				LANMAC:         fmt.Sprintf("52:54:00:%02x:01:%02x", id, index),
			}
			// The first controller in the whole fleet, not the first of each controller
			// group: with both a Rocky and an Ubuntu control plane there are two such
			// groups, and giving each a leader's forwards would have them claim the same
			// host port -- the second qemu then fails to bind and the fleet half starts.
			if !leaderAssigned && g.role == apicluster.RoleController {
				node.HostFwd = leaderForwards(id, spec.Distro)
				leaderAssigned = true
			}
			node.Image = g.img.id
			node.OSID = g.img.osID
			nodes = append(nodes, node)
		}
	}
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
	// Only rke2 serves its join endpoint on a second port. k3s multiplexes onto 6443,
	// and upstream kubeadm has no supervisor at all, so both are covered by the forward
	// above.
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
