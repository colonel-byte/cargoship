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

package tofu

import (
	"context"
	"fmt"
	"os"

	"github.com/colonel-byte/cargoship/internal/microvm"
)

// backendEnvVar selects what provisions the suite's hosts. It is the same variable the cluster
// suite reads and means the same thing here, so a machine configured for one is configured for
// both.
//
// Containers by default, because that is what this suite has always used and what CI can run.
// The microvm backend asks for KVM and spends a minute or two per bring-up where a container
// spends seconds. What it buys is a fleet that reaches itself: the nodes talk over their own
// layer 2 segment rather than through Docker, so a host whose Docker forwards nothing between
// containers -- the thing requireContainerNetworking exists to report -- can still run the walk.
//
// It is not a replacement for the container backend. The containers run Ubuntu and the VMs run
// Rocky, so the two put the provider's output in front of different halves of the phases. Run
// both.
const backendEnvVar = "CARGOSHIP_E2E_BACKEND"

const (
	// backendBootloose provisions the hosts as privileged containers.
	backendBootloose = "bootloose"
	// backendMicroVM provisions them as local virtual machines.
	backendMicroVM = "microvm"
)

// selectedBackend reports which backend this run was asked for. An unrecognised value is an
// error rather than a silent fall back to the default, because the whole point of setting it is
// to get the other one.
func selectedBackend() (string, error) {
	switch v := os.Getenv(backendEnvVar); v {
	case "", backendBootloose:
		return backendBootloose, nil
	case backendMicroVM:
		return backendMicroVM, nil
	default:
		return "", fmt.Errorf("%s=%q is not a backend this suite has; it is %s or %s",
			backendEnvVar, v, backendBootloose, backendMicroVM)
	}
}

// onMicroVM is whether this run provisions virtual machines. A bad value reads as the default
// here and is reported by requireCluster, so that the handful of callers that only need to know
// which way to branch do not each need an error return.
func onMicroVM() bool {
	backend, err := selectedBackend()
	return err == nil && backend == backendMicroVM
}

// vmFleetName scopes the fleet's run directory, its addresses and its ports. It is neither the
// cluster suite's fleet nor dev:vmUp's, because all three derive their loopback ports from this
// name and two fleets sharing one cannot be up at the same time.
const vmFleetName = "tofu"

// vmKC and vmKW are how internal/microvm names the nodes of each role. They are spelled here
// because that package does not export them, and the walk has to name the controller it reads
// the cluster through and the worker it removes.
const (
	vmKC = "kc%d"
	vmKW = "kw%d"
)

// testVMFleet is the brought-up fleet, zero until provisionVMFleet has run.
var testVMFleet microvm.Fleet //nolint:gochecknoglobals

// provisionVMFleet brings up the virtual machines this suite installs onto.
//
// The counts are the container fleet's, for the reason suite.go gives: the removal step needs a
// worker that can go while the cluster keeps running, and a controller that is not the machine
// being removed. The distro is the one enginePackage carries, because it decides which engine
// ports the fleet forwards to the host.
//
// None of the container preflights run first. A VM has its own kernel, so the modules the engine
// needs are the guest image's business rather than this machine's; its /etc/hosts is an ordinary
// file rather than a bind mount Docker owns; and the nodes reach each other over their own
// segment, so Docker's inter-container forwarding never comes into it. microvm.Up checks the
// tools it shells out to itself.
func provisionVMFleet(ctx context.Context) error {
	fleet, err := microvm.Up(ctx, microvm.Spec{
		Fleet:       vmFleetName,
		Controllers: controllers,
		Workers:     workers,
		Distro:      enginePackage.distro,
	})
	if err != nil {
		return fmt.Errorf("bringing up the %s VM fleet: %w", vmFleetName, err)
	}
	testVMFleet = fleet
	return nil
}

// teardownVMFleet takes the fleet away and removes its state, leaving the cached base image.
func teardownVMFleet() error {
	return microvm.Down(vmFleetName, "")
}

// vmFleetVars reads the live machines into the module's `hosts` map and the load balancer
// address, the way fleetVars does for containers.
//
// It goes through microvm.Inventory rather than reading the nodes directly, because that is
// where the private addressing is decided: a node's default route is on the slirp management
// interface, the same address on every node and reachable from none of them, so what the engine
// advertises has to be the address on the fleet's own segment. Reading the inventory is also
// what makes this suite and `cargoship apply --config` hand the phases the same fleet.
func vmFleetVars(f microvm.Fleet) (map[string]hostVars, string, error) {
	if len(f.Nodes) == 0 {
		return nil, "", fmt.Errorf("the %s VM fleet has no nodes", f.Name)
	}
	inventory := microvm.Inventory(f)

	hosts := make(map[string]hostVars, len(inventory.Spec.Hosts))
	for _, host := range inventory.Spec.Hosts {
		ssh := host.ConnectionConfig.SSH
		if ssh == nil {
			return nil, "", fmt.Errorf("the inventory entry for %s carries no SSH configuration", host.Hostname)
		}
		var keyPath string
		if ssh.KeyPath != nil {
			keyPath = *ssh.KeyPath
		}

		hosts[host.Hostname] = hostVars{
			Address:  ssh.Address,
			Role:     host.Role,
			User:     ssh.User,
			Port:     ssh.Port,
			KeyPath:  keyPath,
			Hostname: host.Hostname,
			Profile:  host.Profile,
			// Both are needed here and neither has a container equivalent. Without them every
			// peer address the phases distribute is the slirp address, which no peer can
			// reach -- see microvm.Inventory.
			PrivateAddress:   host.PrivateAddress,
			PrivateInterface: host.PrivateInterface,
		}
	}
	return hosts, inventory.Spec.Config.LoadBalancer, nil
}

// vmExec runs a command on one node over SSH and returns its combined output. It is machineExec
// for this backend, with the same contract: a non-zero exit is an error with the output
// alongside it, because the callers read both.
func vmExec(hostname string, args ...string) (string, error) {
	cmd, err := microvm.SSHCommand(context.Background(), vmFleetName, "", hostname, args...)
	if err != nil {
		return "", err
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}
