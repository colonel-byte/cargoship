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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	apicluster "github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	blcluster "github.com/k0sproject/bootloose/pkg/cluster"
)

const (
	sshPort = 22
	sshUser = "root"
)

// hostVars is one entry of the module's `hosts` map, in the shape its variable declares. Only
// the fields this suite sets are here; the rest are optional in the module and null here.
//
// The keys are what the map is keyed by in the configuration, and the provider uses a key as the
// host's name when `hostname` is not set. This suite sets `hostname` anyway, because the machines
// already have names a `kubectl get nodes` will show and a test that asserted on a key would be
// asserting on its own choice rather than on the cluster.
type hostVars struct {
	Address  string `json:"address"`
	Role     string `json:"role"`
	User     string `json:"user"`
	Port     int    `json:"port"`
	KeyPath  string `json:"key_path"`
	Hostname string `json:"hostname"`
	Profile  string `json:"profile"`
	// State is the lifecycle marker a removal sets. It is omitted while it is empty so that the
	// ordinary case writes no `state` at all, the way a configuration that has never removed
	// anything is written.
	State string `json:"state,omitempty"`
}

// fleetVars reads the live machines and returns the module's `hosts` map and the load balancer
// address, keyed by machine name.
//
// The role comes from the name prefix, the same arrangement the cluster suite's inventory uses:
// a machine whose name starts with the controller prefix is a controller, one with the worker
// prefix is a worker, and anything else is an error rather than a guess.
//
// Every host is reached on 127.0.0.1 through the port Docker published for its SSH port, because
// that is how the machine running the tests reaches a bootloose container. The load balancer is
// the first controller's address on the Docker bridge instead: it is what the nodes use to reach
// each other, and a published port on the host is not routable from inside a node.
func fleetVars(c *blcluster.Cluster, keyPath string) (map[string]hostVars, string, error) {
	machines, err := c.Inspect(nil)
	if err != nil {
		return nil, "", fmt.Errorf("inspecting the bootloose cluster: %w", err)
	}

	hosts := make(map[string]hostVars, len(machines))
	var controllerNames []string
	addresses := map[string]string{}

	for _, m := range machines {
		name := m.Hostname()

		var role string
		switch {
		case strings.HasPrefix(name, controllerPrefix):
			role = apicluster.RoleController
			controllerNames = append(controllerNames, name)
		case strings.HasPrefix(name, workerPrefix):
			role = apicluster.RoleWorker
		default:
			return nil, "", fmt.Errorf("machine %s matches neither the controller nor the worker prefix", name)
		}

		port, err := m.HostPort(sshPort)
		if err != nil {
			return nil, "", fmt.Errorf("reading the published SSH port of %s: %w", name, err)
		}

		hosts[name] = hostVars{
			Address:  "127.0.0.1",
			Role:     role,
			User:     sshUser,
			Port:     port,
			KeyPath:  keyPath,
			Hostname: name,
			// The profile doubles as the node-role.kubernetes.io/<profile> label, which is what
			// makes label_nodes observable from the cluster.
			Profile: role,
		}
		addresses[name] = m.Status().IP
	}

	if len(controllerNames) == 0 {
		return nil, "", fmt.Errorf("no controller machine in the bootloose cluster")
	}
	sort.Strings(controllerNames)

	leader := addresses[controllerNames[0]]
	if leader == "" {
		return nil, "", fmt.Errorf("no docker-bridge address for the first controller %s", controllerNames[0])
	}
	return hosts, leader, nil
}

// controllerPrefix and workerPrefix are the name prefixes fleetVars maps to roles. They are
// derived from the machine templates so the two cannot drift apart.
var (
	controllerPrefix = strings.TrimSuffix(bootKC, "%d") //nolint:gochecknoglobals
	workerPrefix     = strings.TrimSuffix(bootKW, "%d") //nolint:gochecknoglobals
)

// absKeyPath is the private key bootloose generated, as an absolute path. The provider resolves
// a key path in its own process with its own working directory, so a relative one would be read
// against something this suite does not control.
func absKeyPath() (string, error) {
	return filepath.Abs(fleet.Cluster.PrivateKey)
}

// networkProbeName is the throwaway container the networking preflight listens in. It carries the
// cluster's name so a container left by an interrupted run is recognisable, and it is removed
// before and after the probe rather than only after.
const networkProbeName = "cargoship-e2e-tofu-icc-probe"

// networkProbePort is where it listens. Nothing else in the suite uses it.
const networkProbePort = "19347"

// requireContainerNetworking checks that one container can open a TCP connection to another.
//
// A cluster is nodes talking to each other: a worker joins by reaching the controller's
// supervisor, so a host that forwards no traffic between containers cannot run one. Host to
// container works in that situation -- which is why this is worth a preflight rather than being
// obvious. SSH reaches the machines through published ports and the controller installs itself
// locally, so a run gets all the way to the worker join before anything looks wrong, and then
// spends a phase timeout there.
//
// The usual cause is a host firewall: firewalld with Docker's nftables backend drops forwarded
// traffic between containers even with inter-container communication enabled on the bridge, and a
// user-defined network does not escape it.
func requireContainerNetworking() error {
	_ = exec.Command("docker", "rm", "-f", networkProbeName).Run() //nolint:errcheck // absent is the normal case

	listen := fmt.Sprintf("nc -l -p %s -e echo %s", networkProbePort, networkProbeToken)
	if out, err := exec.Command("docker", "run", "-d", "--rm", "--name", networkProbeName, //nolint:gosec
		"--entrypoint", "sh", moduleLoaderImage, "-c", listen,
	).CombinedOutput(); err != nil {
		return fmt.Errorf("starting the container networking probe: %w: %s", err, out)
	}
	defer func() {
		_ = exec.Command("docker", "rm", "-f", networkProbeName).Run() //nolint:errcheck // best-effort cleanup
	}()

	address, err := exec.Command("docker", "inspect", networkProbeName, //nolint:gosec
		"--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}",
	).Output()
	if err != nil {
		return fmt.Errorf("reading the probe's address: %w", err)
	}
	peer := strings.TrimSpace(string(address))
	if peer == "" {
		return fmt.Errorf("the networking probe has no address on any network")
	}

	connect := fmt.Sprintf("nc -w 5 %s %s", peer, networkProbePort)
	out, err := exec.Command("docker", "run", "--rm", "--entrypoint", "sh", moduleLoaderImage, "-c", connect).CombinedOutput() //nolint:gosec
	if err != nil || !strings.Contains(string(out), networkProbeToken) {
		return fmt.Errorf("one container cannot reach another on this machine, so a worker could never join the "+
			"cluster: a TCP connection to %s:%s returned %q (%v).\n"+
			"The usual cause is a host firewall. On a firewalld host, either put the bridge in the docker zone -- "+
			"`sudo firewall-cmd --permanent --zone=docker --add-interface=docker0 && sudo firewall-cmd --reload` -- "+
			"or put Docker back on the iptables firewall backend with {\"firewall-backend\": \"iptables\"} in "+
			"/etc/docker/daemon.json and restart it",
			peer, networkProbePort, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// networkProbeToken is what the probe answers with, so a connection that opened and returned
// something else is not mistaken for a working one.
const networkProbeToken = "cargoship-icc-ok"

// detachHostsFileScript replaces the bind mount Docker puts over /etc/hosts with an ordinary
// file holding the same lines, and does nothing when it is already one.
const detachHostsFileScript = `
if grep -q ' /etc/hosts ' /proc/mounts; then
	cp /etc/hosts /tmp/hosts.detach &&
	umount /etc/hosts &&
	cat /tmp/hosts.detach > /etc/hosts &&
	rm -f /tmp/hosts.detach
fi
`

// detachHostsFile runs that script on every machine.
//
// Docker bind mounts a file it owns over /etc/hosts in every container, and a bind-mounted file
// cannot be replaced from inside, which is what `modify_hosts` does on a real host. The long
// version of why is in the cluster suite's main_test.go; the short version is that leaving the
// mount in place makes the same correct write fail with EBUSY, inconsistently across images.
//
// This suite turns `modify_hosts` on, so it needs the same treatment: that attribute is one of
// the few the module exposes whose effect is visible on the hosts, and running with it off would
// leave the provider's own plumbing for it untested.
func detachHostsFile(c *blcluster.Cluster) error {
	machines, err := c.Inspect(nil)
	if err != nil {
		return fmt.Errorf("listing the cluster's machines: %w", err)
	}
	for _, machine := range machines {
		name := machine.ContainerName()
		out, err := exec.Command("docker", "exec", name, "sh", "-c", detachHostsFileScript).CombinedOutput() //nolint:gosec
		if err != nil {
			return fmt.Errorf("detaching /etc/hosts on %s: %w: %s", name, err, out)
		}
	}
	return nil
}

// kernelModulesDir is the modules tree the loader needs mounted. modprobe looks for it at the
// same path inside the container as outside.
const kernelModulesDir = "/lib/modules"

// moduleLoaderImage is the image the modules are loaded from.
//
// It is not the node image, because the node image has no kmod in it -- no modprobe, no insmod,
// nothing that can load a module. This one is busybox-based and carries modprobe, and it is an
// image the suites already use, so it costs no new dependency.
const moduleLoaderImage = "ghcr.io/colonel-byte/bootloose/alpine-3.23:latest"

// engineKernelModules are the modules the engine needs on the node's kernel.
//
// kube-proxy initialises the legacy iptables `nat` and `filter` tables at startup, and the engine
// shuts itself down when it cannot -- "kube-proxy exited: iptables is not available on this host"
// -- so these are a prerequisite rather than a tuning. The failure it produces is slow and says
// nothing about the cause: the engine starts, notifies readiness, exits cleanly a few seconds
// later, and systemd restarts it for the whole phase timeout.
//
// br_netfilter is what makes bridged traffic traverse those tables, and vxlan is flannel's
// backend. They are often already loaded on a machine that runs containers, which is why this is
// easy to miss: Docker loads them when it uses the iptables backend and does not when it uses
// nftables.
var engineKernelModules = []string{ //nolint:gochecknoglobals
	// keep-sorted start
	"br_netfilter",
	"ip_tables",
	"iptable_filter",
	"iptable_nat",
	"overlay",
	"vxlan",
	// keep-sorted end
}

// loadKernelModulesScript loads each module from inside the loader container.
const loadKernelModulesScript = `
set -e
for m in %s; do
	modprobe "$m" || { echo "modprobe $m failed"; exit 1; }
done
`

// skipModprobeEnvVar turns the loading off, for a machine whose kernel is not the suite's to
// change. The check that follows is not turned off with it: a missing module then fails the run
// naming what to load, rather than being discovered twenty minutes later in a journal.
const skipModprobeEnvVar = "CARGOSHIP_E2E_SKIP_MODPROBE"

func skipModprobe() bool {
	on, err := strconv.ParseBool(os.Getenv(skipModprobeEnvVar))
	return err == nil && on
}

// requireKernelModules makes sure the host kernel carries what the engine needs, loading what it
// does not unless the run was asked to leave the kernel alone.
//
// Nothing is loaded when everything is already there, which is the ordinary case on a machine
// that has run this once or runs containers at all. CARGOSHIP_E2E_SKIP_MODPROBE=1 turns the
// loading off entirely and leaves only the check.
//
// Cargoship does not do any of this itself: the kernel-modules phase belongs to `prepare` rather
// than `apply`, it is gated on the package declaring a `kernel` list, and no shipped definition
// declares one -- so what a node's kernel carries is the machine image's business, which here
// means this suite's. See docs/dev/e2e-tofu-tests.md.
func requireKernelModules() error {
	missing, err := missingKernelModules()
	if err != nil {
		return err
	}
	if len(missing) == 0 {
		return nil
	}

	if skipModprobe() {
		return fmt.Errorf("the kernel on this machine is missing %s, which the engine needs: "+
			"load them with `sudo modprobe %s`, or unset %s and let the suite load them",
			strings.Join(missing, ", "), strings.Join(missing, " "), skipModprobeEnvVar)
	}

	if err := loadKernelModules(missing); err != nil {
		return err
	}

	still, err := missingKernelModules()
	if err != nil {
		return err
	}
	if len(still) > 0 {
		return fmt.Errorf("loaded without error, but the kernel still does not list %s", strings.Join(still, ", "))
	}
	return nil
}

// missingKernelModules are the engine's modules the kernel does not have loaded.
//
// It reads /proc/modules rather than /proc/net/ip_tables_names, because the tables file is per
// network namespace -- empty in a fresh one whatever is loaded -- while the module list is the
// kernel's own and is the same everywhere. The test process runs on the machine itself, so this
// is that machine's kernel without a container in the way.
func missingKernelModules() ([]string, error) {
	body, err := os.ReadFile("/proc/modules")
	if err != nil {
		return nil, fmt.Errorf("reading the loaded kernel modules: %w", err)
	}

	loaded := map[string]bool{}
	for _, line := range strings.Split(string(body), "\n") {
		if name, _, found := strings.Cut(line, " "); found {
			loaded[name] = true
		}
	}

	var missing []string
	for _, module := range engineKernelModules {
		if !loaded[module] {
			missing = append(missing, module)
		}
	}
	return missing, nil
}

// loadKernelModules loads the named modules from a throwaway privileged container.
//
// Modules belong to the host kernel rather than to a container, so one container loads them for
// every machine, and a privileged one with the modules tree mounted is allowed to. The
// alternative is `sudo modprobe` on the machine running the tests, which a test cannot do.
func loadKernelModules(modules []string) error {
	script := fmt.Sprintf(loadKernelModulesScript, strings.Join(modules, " "))

	out, err := exec.Command("docker", "run", "--rm", "--privileged", //nolint:gosec
		"-v", kernelModulesDir+":"+kernelModulesDir+":ro",
		"--entrypoint", "sh", moduleLoaderImage, "-c", script,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("loading the kernel modules the engine needs (%s): %w: %s\n"+
			"load them by hand with `sudo modprobe %s` and set %s=1 to skip this step",
			strings.Join(modules, " "), err, out, strings.Join(modules, " "), skipModprobeEnvVar)
	}
	return nil
}

// machineExec runs a command in the container of the machine with that hostname and returns its
// combined output. It is how the suite checks the cluster without going through the provider: the
// engine's own kubectl is on the controller, and a check that asked the provider would be asking
// the thing under test.
//
// The hostname is not the container name -- bootloose names a container `<cluster>-<machine>` --
// so the machine is looked up rather than named, and a hostname nothing matches is an error
// rather than a `docker exec` that fails for the wrong reason.
func machineExec(hostname string, args ...string) (string, error) {
	container, err := containerFor(hostname)
	if err != nil {
		return "", err
	}
	return machineExecOn(container, args...)
}

// machineExecOn is machineExec for a container whose name is already known.
func machineExecOn(container string, args ...string) (string, error) {
	out, err := exec.Command("docker", append([]string{"exec", container}, args...)...).CombinedOutput() //nolint:gosec
	return string(out), err
}

// containerFor is the container name of the machine with that hostname.
func containerFor(hostname string) (string, error) {
	machines, err := testCluster.Inspect(nil)
	if err != nil {
		return "", fmt.Errorf("listing the cluster's machines: %w", err)
	}
	for _, m := range machines {
		if m.Hostname() == hostname {
			return m.ContainerName(), nil
		}
	}
	return "", fmt.Errorf("no machine in the cluster is named %s", hostname)
}
