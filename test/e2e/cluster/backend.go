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
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	apicluster "github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/internal/microvm"
	"github.com/colonel-byte/cargoship/pkg/packager/load"
)

// backendEnvVar selects what provisions the suite's hosts.
//
// Containers by default, because that is what the suite has always used and what CI can run:
// a bootloose cluster asks nothing of the machine beyond Docker. The microvm backend asks for
// KVM, which a hosted runner does not reliably have, and spends a minute or two per bring-up
// where a container spends seconds.
//
// What it buys is the only thing containers cannot give. A container reports no SELinux, runs
// no fapolicyd and has no firewall of its own, so phases 21, 22 and 26 correctly do nothing
// there -- and phase 21 is fatal on failure, which makes the branch able to stop an apply the
// branch never taken. Against VMs those phases run for real, the *-selinux RPM scriptlets
// execute with a policy to load, and an engine configured `selinux: true` has to actually
// start. See docs/dev/microvm.md.
//
// It is not a replacement for the container backend, and choice-microvm-backend records why:
// the VM fleet runs one distribution, and the OS mix is the substantive part of what the
// bootloose cluster covers. Run both.
const backendEnvVar = "CARGOSHIP_E2E_BACKEND"

const (
	// backendBootloose provisions the hosts as privileged containers.
	backendBootloose = "bootloose"
	// backendMicroVM provisions them as local virtual machines.
	backendMicroVM = "microvm"
	// backendInventory provisions nothing and runs against hosts that already exist, named by
	// an inventory the operator supplies. See inventoryEnvVar.
	backendInventory = "inventory"
)

// inventoryEnvVar names the ZarfCluster document backendInventory runs against.
//
// This is the backend for hosts cargoship does not create: a fleet on AWS, vSphere, Proxmox,
// bare metal, anything already reachable over SSH. The suite provisions nothing, tears nothing
// down, and derives what it asserts from the document rather than from a fleet definition it
// owns -- so it is the only backend whose host count, roles and operating systems are not
// known until the file is read. See docs/dev/e2e-external-inventory.md.
const inventoryEnvVar = "CARGOSHIP_E2E_INVENTORY"

// joinHostEnvVar names the host in that document which the join walk should join.
//
// The join walk needs a host that was not in the cluster when the install ran. The other two
// backends create one on demand; against somebody else's fleet the operator creates it, adds
// it to the document, and names it here. It is then held out of the fleet the apply walk sees
// and put back by withJoinMachine, so every count and assertion downstream reads exactly as it
// does for a fleet this suite provisioned.
//
// Unset means the join walk is skipped, which is the right default: a document naming hosts
// that are all already in the cluster has nothing for it to join.
const joinHostEnvVar = "CARGOSHIP_E2E_JOIN_HOST"

// joinHost is the host the join walk should join, or empty when none was named.
func joinHost() string {
	return os.Getenv(joinHostEnvVar)
}

// backend reports which backend this run was asked for. An unrecognised value is an error
// rather than a silent fall back to the default, because the whole point of setting it is to
// get the other one.
func backend() (string, error) {
	switch v := os.Getenv(backendEnvVar); v {
	case "", backendBootloose:
		return backendBootloose, nil
	case backendMicroVM:
		return backendMicroVM, nil
	case backendInventory:
		return backendInventory, nil
	default:
		return "", fmt.Errorf("%s=%q is not a backend; it is %s, %s or %s",
			backendEnvVar, v, backendBootloose, backendMicroVM, backendInventory)
	}
}

// vmOS is the fleet the microvm backend provisions: one controller and two workers, all Rocky.
//
// There is no upload-only node. That machine exists in the container fleets because rke2 links
// against glibc and the Alpine image is musl, which is the only way the BIN upload phase gets
// exercised as the path it is rather than as a fallback -- see uploadOnlyPrefix. Rocky is
// Enterprise Linux, so a fourth node here would be a second host on the RPM path and would
// cover nothing the first does not.
//
// The image field is empty because the backend has exactly one, pinned in
// internal/microvm/image.go. Giving it a name here would imply a choice that is not offered.
var vmOS = fleetSpec{ //nolint:gochecknoglobals
	name: "cargoship-e2e",
	machines: []machineSpec{
		{nameTemplate: bootKC, osID: "rocky", count: 1},
		{nameTemplate: bootKW, osID: "rocky", count: 2},
	},
}

// microvmSpec is the fleet spec as the microvm backend wants it. The suite's spec is the
// authority on how many of each role there are; everything else is the backend's default.
func microvmSpec(f fleetSpec) microvm.Spec {
	counts := countsFor(f)
	return microvm.Spec{
		Fleet:       "e2e",
		Controllers: counts.controllers,
		Workers:     counts.workers,
		Distro:      distroID(),
	}
}

// vmFleet is the fleet the microvm backend brought up, kept so that teardown can reach it.
var vmFleet *microvm.Fleet //nolint:gochecknoglobals

// provision brings the run's hosts up and renders them as an inventory. It is the one place
// the two backends differ, and everything downstream of the returned inventory is identical
// for both.
func provision(ctx context.Context, spec fleetSpec) (apicluster.ZarfCluster, error) {
	name, err := backend()
	if err != nil {
		return apicluster.ZarfCluster{}, err
	}

	switch name {
	case backendInventory:
		inv, err := readInventory()
		if err != nil {
			return apicluster.ZarfCluster{}, err
		}
		return hostsNamedBy(inv, spec), nil

	case backendMicroVM:
		fleet, err := microvm.Up(ctx, microvmSpec(spec))
		if err != nil {
			return apicluster.ZarfCluster{}, err
		}
		vmFleet = &fleet
		return microvm.Inventory(fleet), nil

	default:
		cluster, err := setup(spec)
		if err != nil {
			return apicluster.ZarfCluster{}, err
		}
		testCluster = cluster
		return bootlooseInventory(cluster, spec)
	}
}

// teardown removes whatever provision brought up.
func teardown() error {
	name, err := backend()
	if err != nil {
		return err
	}

	switch name {
	case backendInventory:
		// The hosts were not created here, so they are not destroyed here either. A run
		// against somebody's cluster that deleted it on the way out would be a trap.
		return nil

	case backendMicroVM:
		if vmFleet == nil {
			return nil
		}
		return microvm.Down(vmFleet.Name, "")
	default:
		if testCluster == nil {
			return nil
		}
		return shutdown(testCluster)
	}
}

// OS families a fleet can carry, as the upload phases divide them. These are the families
// pkg/utils/os.go routes on.
const (
	familyEnterpriseLinux = "el"
	familyDebian          = "debian"
	familyOther           = "other"
)

// familyByOSID maps an os-release ID a replica group declares to the family the upload phases
// route it into. Anything absent is familyOther, which is the BIN upload path.
var familyByOSID = map[string]string{ //nolint:gochecknoglobals
	"ubuntu": familyDebian,
	"debian": familyDebian,
	"fedora": familyEnterpriseLinux,
	"rocky":  familyEnterpriseLinux,
}

// carriesFamily reports whether the fleet declares a host of the given family.
//
// The family-specific upload tests guard against a cluster that was meant to cover a branch
// and quietly stopped: without a Debian host, the APT phase is exercised only as a phase that
// declines every host. That guard is right for the container fleets and wrong for a fleet that
// never claimed the family -- a single-distribution fleet has nothing for the other branch to
// claim, and failing it would report a gap its own definition already states. This is the same
// shape binUploadFiles already uses for uploadOnlyCount.
func carriesFamily(f fleetSpec, family string) bool {
	for id := range f.osIDs() {
		got, ok := familyByOSID[id]
		if !ok {
			got = familyOther
		}
		if got == family {
			return true
		}
	}
	return false
}

// readInventory loads the operator's ZarfCluster document.
//
// It goes through the same loader the CLI uses rather than a plain unmarshal, so that a
// document this backend accepts is one cargoship accepts: the inventory the suite is handed
// and the inventory a real run would be handed are parsed by the same code.
func readInventory() (apicluster.ZarfCluster, error) {
	path := os.Getenv(inventoryEnvVar)
	if path == "" {
		return apicluster.ZarfCluster{}, fmt.Errorf("%s=%s needs %s set to a ZarfCluster document",
			backendEnvVar, backendInventory, inventoryEnvVar)
	}

	inv, err := load.ClusterDefinition(context.Background(), path, load.ClusterOptions{})
	if err != nil {
		return apicluster.ZarfCluster{}, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(inv.Spec.Hosts) == 0 {
		return apicluster.ZarfCluster{}, fmt.Errorf("%s names no hosts", path)
	}
	if len(inv.Spec.Hosts.WithRole(apicluster.RoleController)) == 0 {
		return apicluster.ZarfCluster{}, fmt.Errorf("%s names no controller, so there is nothing to install a control plane on", path)
	}
	return inv, nil
}

// inventoryFleet is the fleet spec for an externally supplied inventory, read once.
var inventoryFleet struct { //nolint:gochecknoglobals
	once sync.Once
	spec fleetSpec
	err  error
}

// externalFleet derives a fleet spec from the operator's inventory.
//
// Every group holds exactly one host, named literally rather than by a template, because an
// external inventory's hostnames are whatever the operator's fleet calls them -- `ip-10-0-1-5`,
// a vSphere VM name -- and nothing about them is a pattern this suite gets to choose.
//
// No group declares an osID. The operating systems are not knowable until phase 09 has asked
// the hosts, so the assertions that compare against a declared OS, and the family guards on the
// upload phases, all stand down for this backend rather than asserting against a guess.
func externalFleet() (fleetSpec, error) {
	inventoryFleet.once.Do(func() {
		inv, err := readInventory()
		if err != nil {
			inventoryFleet.err = err
			return
		}
		join := joinHost()
		spec := specFromInventory(inv, join)
		if join != "" && spec.joinGroup == nil {
			inventoryFleet.err = fmt.Errorf("%s=%s names no host in %s",
				joinHostEnvVar, join, os.Getenv(inventoryEnvVar))
			return
		}
		if len(spec.machines) == 0 {
			inventoryFleet.err = fmt.Errorf("%s=%s is the only host in %s, so there is no cluster for it to join",
				joinHostEnvVar, join, os.Getenv(inventoryEnvVar))
			return
		}
		inventoryFleet.spec = spec
	})
	return inventoryFleet.spec, inventoryFleet.err
}

// hostsNamedBy narrows a document to the hosts a spec names, preserving the document's own
// ordering so the leader stays first.
//
// This is what holds the join host out of the apply walk and lets the join walk have it: both
// read the same file, and the spec they are given is the difference. A spec naming every host
// -- which is every spec the other backends produce -- returns the document unchanged.
func hostsNamedBy(inv apicluster.ZarfCluster, spec fleetSpec) apicluster.ZarfCluster {
	wanted := make(map[string]bool, len(spec.machines))
	for _, name := range spec.hostnames() {
		wanted[name] = true
	}
	inv.Spec.Hosts = inv.Spec.Hosts.Filter(func(h *apicluster.ZarfHost) bool {
		return wanted[h.Hostname]
	})
	return inv
}

// specFromInventory derives a fleet from a document, holding out the host named by join.
//
// Separate from externalFleet because that memoises: this is the whole derivation and takes
// its inputs as arguments, so it can be exercised over several documents in one process.
func specFromInventory(inv apicluster.ZarfCluster, join string) fleetSpec {
	spec := fleetSpec{name: inv.Metadata.Name}
	for _, h := range inv.Spec.Hosts {
		group := machineSpec{
			nameTemplate: h.Hostname,
			role:         h.Role,
			count:        1,
		}
		if join != "" && h.Hostname == join {
			spec.joinGroup = &group
			continue
		}
		spec.machines = append(spec.machines, group)
	}
	return spec
}

// provisions reports whether the backend creates the hosts it runs against.
//
// The join walk needs a host that did not exist when the install ran, which only a backend
// that creates hosts can produce. Against an external inventory it is skipped rather than
// failed: the suite has no way to add a node to somebody else's fleet, and saying so is more
// use than a failure that looks like a product defect.
func provisions() bool {
	name, err := backend()
	return err == nil && name != backendInventory
}

// canJoin reports whether the join walk has a host to join: one the backend will create, or
// one the operator created and named.
func canJoin() bool {
	return provisions() || joinHost() != ""
}

// apiDialTimeout bounds the reachability probe below. It is a TCP connect to a host that is
// either a few milliseconds away or not reachable at all, so this only has to be long enough
// not to misreport a loaded machine.
const apiDialTimeout = 5 * time.Second

// reachesClusterAPI reports whether this machine can open a connection to the cluster's API
// server at the address the inventory names.
//
// Most of the suite drives the hosts over SSH, which is all any backend guarantees. Three
// steps instead talk to Kubernetes from here -- the kubeconfig the install writes, the node
// labelling that reads it, and the health check that counts Ready nodes -- and those need a
// route to the load balancer address, which is a property of the environment rather than of
// cargoship.
//
// The VM fleet does not have one by construction: its load balancer is the leader's address on
// a multicast segment private to the qemu processes, because that is the address the other
// nodes join through, and once the firewall phase has run the only port reachable from here is
// ssh. The container fleet does have one, the docker-bridge address.
//
// So it is probed rather than declared per backend. A probe says the same thing for every
// backend, needs no flag, and cannot drift from what is actually true of the machine the suite
// is running on.
func reachesClusterAPI(loadBalancer string) bool {
	if loadBalancer == "" {
		return false
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(loadBalancer, "6443"), apiDialTimeout)
	if err != nil {
		return false
	}
	conn.Close() //nolint:errcheck // the probe is the dial; a close error says nothing about reachability
	return true
}
