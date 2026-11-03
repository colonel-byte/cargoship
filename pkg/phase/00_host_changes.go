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

package phase

import (
	"sort"
	"sync"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
)

// hostChanges collects what a phase did, host by host.
//
// Most phases act on the fleet through one of the parallel helpers on GenericPhase, so the
// bookkeeping behind Changed and HostsChanged is the same everywhere: a mutex, a map keyed by
// host pointer, and the discipline of recording the hosts that did not change as well as the
// ones that did. Embedding this is what keeps that discipline from being re-derived, slightly
// differently, in every phase that reports.
//
// The zero value is ready to use.
type hostChanges struct {
	mu      sync.Mutex
	changes map[*cluster.Host]*HostChange
}

// observed records that the phase looked at a host and is not reporting a change to it.
//
// Calling this is the difference between "checked and found correct" and "never looked", which
// are the same absence in a report that only records changes. A phase that reports per host is
// expected to call it for every host it touched, including on the paths where it changed
// nothing.
func (c *hostChanges) observed(h *cluster.Host) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entry(h)
}

// changedHost records that the phase changed a host, with no detail about what.
func (c *hostChanges) changedHost(h *cluster.Host) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entry(h).Changed = true
}

// changedFile records that the phase changed one file on a host.
//
// diff may be empty, and means the prior content was never read rather than that it did not
// differ -- several phases compare by checksum or write unconditionally.
func (c *hostChanges) changedFile(h *cluster.Host, path, reason, diff string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entry(h)
	e.Changed = true
	e.Details = append(e.Details, HostChangeDetail{
		Path:   path,
		Reason: reason,
		Diff:   diff,
	})
}

// entry returns the record for a host, creating it on first mention. The caller holds the mutex.
func (c *hostChanges) entry(h *cluster.Host) *HostChange {
	if c.changes == nil {
		c.changes = map[*cluster.Host]*HostChange{}
	}
	e, ok := c.changes[h]
	if !ok {
		e = &HostChange{Host: h.ConfiguredAddress()}
		c.changes[h] = e
	}
	return e
}

// Changed reports whether the phase changed any host. See changedReporter in 05_manager.go.
func (c *hostChanges) Changed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.changes {
		if e.Changed {
			return true
		}
	}
	return false
}

// HostsChanged reports what the phase did to each host it touched. See hostChangedReporter in
// 05_manager.go.
//
// The order is by address rather than by the order the hosts were reached, which is whatever the
// parallel sweep happened to finish in and would differ between two identical runs.
func (c *hostChanges) HostsChanged() []HostChange {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]HostChange, 0, len(c.changes))
	for _, e := range c.changes {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out
}
