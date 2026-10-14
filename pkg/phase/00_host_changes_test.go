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
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHostChangesSeparatesLookedFromChanged is the distinction the whole type exists for. A
// report that only records changes cannot tell a host that was checked and found correct from
// one nothing ever looked at, and only one of those means the fleet was assessed.
func TestHostChangesSeparatesLookedFromChanged(t *testing.T) {
	var c hostChanges

	quiet := hostWithAddress("10.0.0.1")
	moved := hostWithAddress("10.0.0.2")

	c.observed(quiet)
	c.changedFile(moved, "/etc/hosts", reasonOutOfSync, "--- a\n+++ b\n")

	require.True(t, c.Changed())

	got := c.HostsChanged()
	require.Len(t, got, 2)

	require.Equal(t, "10.0.0.1", got[0].Host)
	require.False(t, got[0].Changed)
	require.Empty(t, got[0].Details)

	require.Equal(t, "10.0.0.2", got[1].Host)
	require.True(t, got[1].Changed)
	require.Len(t, got[1].Details, 1)
	require.Equal(t, "/etc/hosts", got[1].Details[0].Path)
}

// TestHostChangesReportsNothingWhenNothingLooked pins that an untouched phase declares no hosts
// rather than an empty fleet, and is not Changed.
func TestHostChangesReportsNothingWhenNothingLooked(t *testing.T) {
	var c hostChanges
	require.False(t, c.Changed())
	require.Empty(t, c.HostsChanged())
}

// TestHostChangesObservedDoesNotDowngradeAChange pins the ordering hazard. Phases record from a
// parallel sweep, so an observed call can land after a change on the same host, and it must not
// erase it.
func TestHostChangesObservedDoesNotDowngradeAChange(t *testing.T) {
	var c hostChanges
	h := hostWithAddress("10.0.0.1")

	c.changedHost(h)
	c.observed(h)

	require.True(t, c.Changed())
	require.True(t, c.HostsChanged()[0].Changed)
}

// TestHostChangesIsSafeUnderTheParallelHelpers pins that the type is usable from the fan-out
// helpers on GenericPhase, which is the only way phases ever touch a fleet. Run with -race.
func TestHostChangesIsSafeUnderTheParallelHelpers(t *testing.T) {
	var c hostChanges
	hosts := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4"}

	var wg sync.WaitGroup
	for _, address := range hosts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h := hostWithAddress(address)
			c.observed(h)
			c.changedFile(h, "/etc/sysctl.d/99-cargoship.conf", reasonOutOfSync, "")
		}()
	}
	wg.Wait()

	require.True(t, c.Changed())
	require.Len(t, c.HostsChanged(), len(hosts))
}
