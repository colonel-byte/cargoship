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

package action

import (
	"testing"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/pkg/phase"
	"github.com/stretchr/testify/require"
)

// newTestApply builds an apply action the way a command does, with nothing in the config: the
// phase list NewApply returns does not depend on the hosts, so the assertions below need no
// cluster.
func newTestApply(t *testing.T) *Apply {
	t.Helper()

	a := NewApply(ApplyOptions{
		Manager: &phase.Manager{
			DistroID: "k3s",
			Config:   &cluster.ZarfCluster{},
		},
	})
	require.NotNil(t, a, "NewApply returned nil, which it only does for an unknown distro")
	return a
}

// TestApplyChecksRunBeforeTheLock is what protects the ordering #307 asks for. Every phase ahead
// of Lock has to be read-only, because Lock writes a lock file to every host in the config and
// the refusals ahead of it -- a downgrade, a node the config no longer holds -- are only free to
// stop at if nothing has been written yet.
//
// Lock itself declares neither readOnly nor withDryRun, deliberately, so the test asserts against
// the first phase that is not read-only rather than against Lock's type.
func TestApplyChecksRunBeforeTheLock(t *testing.T) {
	a := newTestApply(t)

	var lockIndex = -1
	for i, p := range a.Phases {
		if _, ok := p.(*phase.Lock); ok {
			lockIndex = i
			break
		}
	}
	require.NotEqual(t, -1, lockIndex, "apply no longer takes a lock")

	for _, p := range a.Phases[:lockIndex] {
		require.Equal(
			t,
			phase.DryRunReadOnly,
			phase.ClassifyDryRun(p),
			"%s runs before the lock, so it has to be read-only", p.Title(),
		)
	}
}

// TestApplyRefusesADowngradeBeforeTheLock names the two phases the ordering above exists for, so
// that moving either of them back behind the lock fails here rather than in a cluster run.
func TestApplyRefusesADowngradeBeforeTheLock(t *testing.T) {
	a := newTestApply(t)

	var sawFactsDistro, sawRemovedHosts bool
	for _, p := range a.Phases {
		switch p.(type) {
		case *phase.GatherFactsDistro:
			sawFactsDistro = true
		case *phase.DetectRemovedHosts:
			sawRemovedHosts = true
		case *phase.Lock:
			require.True(t, sawFactsDistro, "GatherFactsDistro runs after the lock")
			require.True(t, sawRemovedHosts, "DetectRemovedHosts runs after the lock")
			return
		}
	}
	t.Fatal("apply no longer takes a lock")
}

// TestApplyPassesAllowDowngrade pins the flag reaching the phase that reads it. The option was a
// struct field wired to nothing for long enough that the dead end is worth a test.
func TestApplyPassesAllowDowngrade(t *testing.T) {
	a := NewApply(ApplyOptions{
		Manager: &phase.Manager{
			DistroID: "k3s",
			Config:   &cluster.ZarfCluster{},
		},
		AllowDowngrade: true,
	})
	require.NotNil(t, a)

	for _, p := range a.Phases {
		if facts, ok := p.(*phase.GatherFactsDistro); ok {
			require.True(t, facts.AllowDowngrade)
			return
		}
	}
	t.Fatal("apply no longer gathers distro facts")
}
