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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// defaultRunRoot is where fleet state is kept.
//
// Not TMPDIR. An overlay reaches a quarter of a gigabyte after one boot and one dnf
// transaction, and TMPDIR here is routinely a tmpfs -- the repository's own .envrc points it
// at one. build/ is already the repository's scratch directory: .envrc puts it on PATH and
// the e2e runner writes build/tmp, so it is gitignored, on real storage, and removed by
// dev:clean along with everything else that is not source.
const defaultRunRoot = "build/microvm"

// stopGrace is how long a node gets to shut down after SIGTERM before it is killed. qemu
// exits on the signal almost at once; the wait is for the rare case where it is mid-write.
const stopGrace = 30 * time.Second

// runRoot resolves where a spec's fleets are kept.
func (s Spec) runRoot() string {
	if s.RunRoot != "" {
		return s.RunRoot
	}
	return defaultRunRoot
}

// dir is the fleet's own directory.
func (s Spec) dir() string {
	return filepath.Join(s.runRoot(), s.Fleet)
}

// Fleet is a brought-up fleet: where its state lives, how to reach it, and what is in it.
type Fleet struct {
	// Name is the fleet name.
	Name string
	// Dir is the fleet's state directory.
	Dir string
	// KeyPath is the private half of the key every node trusts for root.
	KeyPath string
	// Nodes are the fleet's nodes, controllers first.
	Nodes []Node
	// Distro is the engine the fleet was prepared for, which decides the forwards.
	Distro string
}

// nodeDir is where one node's overlay, seed, pidfile and console log live.
func (f Fleet) nodeDir(n Node) string {
	return filepath.Join(f.Dir, n.Name)
}

// Node returns the named node.
func (f Fleet) Node(name string) (Node, error) {
	for _, n := range f.Nodes {
		if n.Name == name {
			return n, nil
		}
	}
	names := make([]string, 0, len(f.Nodes))
	for _, n := range f.Nodes {
		names = append(names, n.Name)
	}
	return Node{}, fmt.Errorf("no node %q in fleet %s; it has %s", name, f.Name, strings.Join(names, ", "))
}

// InventoryPath is where Up writes the fleet's ZarfCluster inventory.
func (f Fleet) InventoryPath() string {
	return filepath.Join(f.Dir, "inventory.yaml")
}

// Status is one node's liveness, as List reports it.
type Status struct {
	Node
	// PID is the qemu process, or zero when the node is not running.
	PID int
	// Running is whether that process is still there.
	Running bool
}

// readPID reads a node's recorded qemu pid. A missing pidfile is reported as zero with no
// error: a node that was never started and a node that has been torn down are the same thing
// to every caller here.
func readPID(dir string) (int, error) {
	b, err := os.ReadFile(filepath.Join(dir, "pid"))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", filepath.Join(dir, "pid"), err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, fmt.Errorf("parsing %s: %w", filepath.Join(dir, "pid"), err)
	}
	return pid, nil
}

// alive reports whether pid is a live process this user can signal.
//
// Signal 0 is the check rather than an entry in /proc, because the answer wanted is "can this
// be signalled", which is what Stop goes on to do. A pid that has been recycled by another of
// the user's processes reads as alive, which is the safe direction: the alternative is
// reporting a node as down and leaving its qemu running.
func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// stop ends a node's qemu process and removes its directory.
func stop(dir string) error {
	pid, err := readPID(dir)
	if err != nil {
		return err
	}
	if alive(pid) {
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("signalling qemu %d: %w", pid, err)
		}
		deadline := time.Now().Add(stopGrace)
		for alive(pid) && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
		}
		if alive(pid) {
			if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
				return fmt.Errorf("killing qemu %d after %s: %w", pid, stopGrace, err)
			}
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("removing %s: %w", dir, err)
	}
	return nil
}
