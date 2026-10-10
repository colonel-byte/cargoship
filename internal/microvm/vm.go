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
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// requiredTools are the executables a fleet needs. All of them come with a desktop Linux
// install that can run a virtual machine at all; none of them needs root.
var requiredTools = []string{"qemu-system-x86_64", "qemu-img", "mkfs.vfat", "mcopy", "ssh"} //nolint:gochecknoglobals

// sshTimeout bounds the wait for a node's sshd. A node reaches sshd in well under a minute --
// six seconds is typical, since cloud-init's work here is small -- so this is long enough to
// absorb a cold page cache and short enough to fail while the console log still explains why.
const sshTimeout = 3 * time.Minute

// Up brings a fleet up and returns it, with every node answering SSH and the inventory
// written. It refuses a fleet that is already up rather than adopting it, so that a bring-up
// always starts from a fresh overlay: a node carrying the state of a previous run is the one
// thing that makes a phase failure impossible to reason about.
func Up(ctx context.Context, spec Spec) (Fleet, error) {
	spec, err := spec.Normalize()
	if err != nil {
		return Fleet{}, err
	}
	if err := checkTools(); err != nil {
		return Fleet{}, err
	}
	nodes, err := spec.Nodes()
	if err != nil {
		return Fleet{}, err
	}
	if err := checkMemory(spec, len(nodes)); err != nil {
		return Fleet{}, err
	}

	fleet := Fleet{
		Name:   spec.Fleet,
		Dir:    spec.dir(),
		Nodes:  nodes,
		Distro: spec.Distro,
	}
	if running, err := anyRunning(fleet); err != nil {
		return Fleet{}, err
	} else if len(running) > 0 {
		return Fleet{}, fmt.Errorf("fleet %s is already up (%s); tear it down first with `mage dev:vmDown`", fleet.Name, strings.Join(running, ", "))
	}

	base, err := Fetch(ctx)
	if err != nil {
		return Fleet{}, err
	}
	if err := os.RemoveAll(fleet.Dir); err != nil {
		return Fleet{}, fmt.Errorf("clearing %s: %w", fleet.Dir, err)
	}
	if err := os.MkdirAll(fleet.Dir, 0o755); err != nil {
		return Fleet{}, fmt.Errorf("creating %s: %w", fleet.Dir, err)
	}

	fleet.KeyPath = filepath.Join(fleet.Dir, "key")
	publicKey, err := writeKeyPair(fleet.KeyPath)
	if err != nil {
		return Fleet{}, err
	}

	for _, node := range fleet.Nodes {
		if err := start(ctx, spec, fleet, node, base, publicKey); err != nil {
			// A half-started fleet is not something to leave for the developer to clean up,
			// and it is not something a later Up would adopt either, since Up refuses a live
			// fleet. Tear it down and report the original failure.
			Down(fleet.Name, spec.RunRoot) //nolint:errcheck // the start failure is the one worth reporting
			return Fleet{}, err
		}
	}

	for _, node := range fleet.Nodes {
		if err := waitForSSH(ctx, fleet, node); err != nil {
			return Fleet{}, err
		}
	}

	if err := writeInventory(fleet); err != nil {
		return Fleet{}, err
	}
	return fleet, nil
}

// Down tears a fleet down and removes its state. It is not an error to tear down a fleet that
// is not running: that is the state Down is asked to reach.
func Down(fleet, runRoot string) error {
	spec, err := Spec{
		Fleet:       fleet,
		Controllers: 1,
		RunRoot:     runRoot,
	}.Normalize()
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(spec.dir())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", spec.dir(), err)
	}

	// Every directory is stopped, rather than only the nodes a spec would derive: the fleet
	// on disk is the authority on what is running, and a Down called with a different node
	// count than the Up that created it still has to leave nothing behind.
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if err := stop(filepath.Join(spec.dir(), e.Name())); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(spec.dir()); err != nil {
		return fmt.Errorf("removing %s: %w", spec.dir(), err)
	}
	return nil
}

// List reports the fleet's nodes and whether each is running. It reads the fleet from disk, so
// it describes what is actually there rather than what a spec would have created.
func List(fleet, runRoot string) ([]Status, error) {
	spec, err := Spec{
		Fleet:       fleet,
		Controllers: 1,
		RunRoot:     runRoot,
	}.Normalize()
	if err != nil {
		return nil, err
	}

	f, err := load(spec)
	if err != nil {
		return nil, err
	}

	out := make([]Status, 0, len(f.Nodes))
	for _, n := range f.Nodes {
		pid, err := readPID(f.nodeDir(n))
		if err != nil {
			return nil, err
		}
		out = append(out, Status{
			Node:    n,
			PID:     pid,
			Running: alive(pid),
		})
	}
	return out, nil
}

// SSHCommand is the ssh invocation that reaches a node, as a command ready to run with the
// caller's stdio attached.
//
// Host key checking is off and the known-hosts file is /dev/null. These nodes generate a new
// host key on every bring-up, so a known-hosts entry is wrong by construction -- and writing
// one would put a key for 127.0.0.1 on a port the next fleet reuses into the developer's real
// known_hosts.
func SSHCommand(ctx context.Context, fleet, runRoot, node string, command ...string) (*exec.Cmd, error) {
	spec, err := Spec{
		Fleet:       fleet,
		Controllers: 1,
		RunRoot:     runRoot,
	}.Normalize()
	if err != nil {
		return nil, err
	}
	f, err := load(spec)
	if err != nil {
		return nil, err
	}
	n, err := f.Node(node)
	if err != nil {
		return nil, err
	}

	args := append(sshOptions(f.KeyPath),
		"-p", strconv.Itoa(n.SSHPort),
		"root@127.0.0.1",
	)
	return exec.CommandContext(ctx, "ssh", append(args, command...)...), nil
}

// sshOptions are the flags every ssh into a fleet node carries.
func sshOptions(keyPath string) []string {
	return []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-i", keyPath,
	}
}

// load reconstructs a fleet from its directory, deriving each node's addresses and ports from
// its name the same way Nodes did. Nothing per-node is stored: the derivation is the record.
func load(spec Spec) (Fleet, error) {
	entries, err := os.ReadDir(spec.dir())
	if errors.Is(err, os.ErrNotExist) {
		return Fleet{}, fmt.Errorf("no fleet %s under %s; bring one up with `mage dev:vmUp`", spec.Fleet, spec.runRoot())
	}
	if err != nil {
		return Fleet{}, fmt.Errorf("reading %s: %w", spec.dir(), err)
	}

	var controllers, workers int
	for _, e := range entries {
		switch {
		case !e.IsDir():
		case strings.HasPrefix(e.Name(), controllerPrefix):
			controllers++
		case strings.HasPrefix(e.Name(), workerPrefix):
			workers++
		}
	}
	if controllers == 0 {
		return Fleet{}, fmt.Errorf("fleet %s under %s has no controller directory", spec.Fleet, spec.runRoot())
	}

	spec.Controllers = controllers
	spec.Workers = workers
	nodes, err := spec.Nodes()
	if err != nil {
		return Fleet{}, err
	}
	return Fleet{
		Name:    spec.Fleet,
		Dir:     spec.dir(),
		KeyPath: filepath.Join(spec.dir(), "key"),
		Nodes:   nodes,
		Distro:  spec.Distro,
	}, nil
}

// anyRunning names the fleet's nodes that already have a live qemu.
func anyRunning(f Fleet) ([]string, error) {
	var running []string
	for _, n := range f.Nodes {
		pid, err := readPID(f.nodeDir(n))
		if err != nil {
			return nil, err
		}
		if alive(pid) {
			running = append(running, n.Name)
		}
	}
	return running, nil
}

// start creates a node's overlay and seed and launches its qemu.
func start(ctx context.Context, spec Spec, f Fleet, n Node, base, publicKey string) error {
	dir := f.nodeDir(n)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	overlay := filepath.Join(dir, "overlay.qcow2")
	out, err := exec.CommandContext(ctx, "qemu-img", "create", "-q",
		"-f", "qcow2", "-b", base, "-F", "qcow2", overlay, spec.DiskSize).CombinedOutput()
	if err != nil {
		return fmt.Errorf("creating the overlay for %s: %w: %s", n.Name, err, out)
	}

	if _, err := writeSeed(dir, n, publicKey); err != nil {
		return err
	}

	args := qemuArgs(spec, n, dir)
	if out, err := exec.CommandContext(ctx, "qemu-system-x86_64", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("starting %s: %w: %s", n.Name, err, out)
	}
	return nil
}

// qemuArgs assembles the command line for one node.
//
// Two details here are not obvious and cost a boot each to find. -display none, not
// -nographic: qemu refuses -nographic together with -daemonize, and says so only once the
// process is launched. And cache=unsafe on the overlay, which is correct precisely because
// these disks are disposable -- there is no state on them worth an fsync, and the overlay is
// recreated on the next bring-up regardless.
func qemuArgs(spec Spec, n Node, dir string) []string {
	id := fleetID(spec.Fleet)

	hostfwd := []string{fmt.Sprintf("hostfwd=tcp:127.0.0.1:%d-:22", n.SSHPort)}
	for _, f := range n.HostFwd {
		hostfwd = append(hostfwd, fmt.Sprintf("hostfwd=tcp:127.0.0.1:%d-:%d", f.HostPort, f.GuestPort))
	}

	return []string{
		"-machine", "q35,accel=kvm",
		"-cpu", "host",
		"-smp", strconv.Itoa(spec.CPUs),
		"-m", strconv.Itoa(spec.MemoryMiB),
		"-display", "none",
		"-monitor", "none",
		"-serial", "file:" + filepath.Join(dir, "console.log"),
		"-drive", "if=virtio,file=" + filepath.Join(dir, "overlay.qcow2") + ",format=qcow2,cache=unsafe",
		"-drive", "if=virtio,file=" + filepath.Join(dir, "seed.img") + ",format=raw,readonly=on",
		"-device", "virtio-rng-pci",

		// The management interface. slirp needs no privileges and gives two things: the
		// loopback forwards above, and outbound access, which phase 21 depends on -- it
		// installs container-selinux by name and is fatal if it cannot.
		"-netdev", "user,id=mgmt," + strings.Join(hostfwd, ","),
		"-device", "virtio-net-pci,netdev=mgmt,mac=" + n.MgmtMAC,

		// The fleet's own layer 2 segment. A socket netdev on a multicast group joins every
		// qemu process in the fleet onto one broadcast domain with no tap device, no bridge
		// and no root -- and, because it is loopback UDP between processes, nothing for the
		// host's firewall to have an opinion about. That last part is why it is here rather
		// than a bridge: on a host running firewalld with Docker's nftables backend,
		// container-to-container traffic is silently dropped, and a bridge would be subject
		// to the same ruleset.
		"-netdev", "socket,id=lan,mcast=" + mcastFor(id) + ",localaddr=127.0.0.1",
		"-device", "virtio-net-pci,netdev=lan,mac=" + n.LANMAC,

		"-daemonize",
		"-pidfile", filepath.Join(dir, "pid"),
	}
}

// waitForSSH blocks until a node answers SSH, or reports the tail of its console log.
func waitForSSH(ctx context.Context, f Fleet, n Node) error {
	ctx, cancel := context.WithTimeout(ctx, sshTimeout)
	defer cancel()

	for {
		cmd, err := sshProbe(ctx, f, n)
		if err != nil {
			return err
		}
		if cmd.Run() == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s did not answer ssh within %s; the tail of %s:\n%s",
				n.Name, sshTimeout, filepath.Join(f.nodeDir(n), "console.log"), consoleTail(f.nodeDir(n)))
		case <-time.After(time.Second):
		}
	}
}

// sshProbe is one connection attempt against a node, used only for its exit status.
func sshProbe(ctx context.Context, f Fleet, n Node) (*exec.Cmd, error) {
	args := append(sshOptions(f.KeyPath),
		"-o", "ConnectTimeout=2",
		"-p", strconv.Itoa(n.SSHPort),
		"root@127.0.0.1",
		"true",
	)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd, nil
}

// consoleTail is the last few lines of a node's serial console, which is where the reason a
// node never reached sshd is written. Nothing else records it: qemu is detached and its own
// output went to the pidfile.
func consoleTail(dir string) string {
	const lines = 40

	b, err := os.ReadFile(filepath.Join(dir, "console.log"))
	if err != nil {
		return fmt.Sprintf("(could not read the console log: %v)", err)
	}
	split := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(split) > lines {
		split = split[len(split)-lines:]
	}
	return strings.Join(split, "\n")
}

// checkTools reports every missing executable at once, rather than failing on the first one
// part way through a bring-up.
func checkTools() error {
	var missing []string
	for _, tool := range requiredTools {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing %s; on Fedora they come from qemu-system-x86, qemu-img, dosfstools, mtools and openssh-clients", strings.Join(missing, ", "))
	}
	if _, err := os.Stat("/dev/kvm"); err != nil {
		return fmt.Errorf("/dev/kvm is not available, so a fleet would fall back to emulation and be unusably slow: %w", err)
	}
	return nil
}

// writeKeyPair generates the fleet's SSH key and returns the public half.
//
// A key per fleet, not a key per developer: it is generated by the bring-up, lives in the
// fleet directory, and goes away with it. Nothing outside the fleet ever trusts it.
func writeKeyPair(path string) (string, error) {
	out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "",
		"-C", "cargoship-microvm", "-f", path).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("generating the fleet key at %s: %w: %s", path, err, out)
	}
	pub, err := os.ReadFile(path + ".pub")
	if err != nil {
		return "", fmt.Errorf("reading the generated public key: %w", err)
	}
	return strings.TrimSpace(string(pub)), nil
}

// checkMemory refuses a fleet that would not fit in the memory the host has available.
//
// Letting it through means the OOM killer picks which process dies, and on a developer's
// machine that is as likely to be their editor as it is to be a node. The check is against
// MemAvailable rather than MemTotal, so it accounts for what is already running.
func checkMemory(spec Spec, nodes int) error {
	wantMiB := spec.MemoryMiB * nodes
	availMiB, err := availableMemoryMiB()
	if err != nil {
		// Not being able to read /proc/meminfo is not a reason to refuse to start.
		return nil //nolint:nilerr
	}
	if wantMiB > availMiB {
		return fmt.Errorf("a %d node fleet at %dMiB each needs %dMiB, and only %dMiB is available; lower the per-node memory or run fewer nodes",
			nodes, spec.MemoryMiB, wantMiB, availMiB)
	}
	return nil
}

// availableMemoryMiB reads MemAvailable from /proc/meminfo.
func availableMemoryMiB() (int, error) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		rest, ok := strings.CutPrefix(line, "MemAvailable:")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return 0, fmt.Errorf("could not parse MemAvailable from %q", line)
		}
		kib, err := strconv.Atoi(fields[0])
		if err != nil {
			return 0, fmt.Errorf("could not parse MemAvailable from %q: %w", line, err)
		}
		return kib / 1024, nil
	}
	return 0, errors.New("no MemAvailable line in /proc/meminfo")
}
