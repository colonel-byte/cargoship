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

	a, err := NewApply(ApplyOptions{
		Manager: &phase.Manager{
			DistroID: "k3s",
			Config:   &cluster.ZarfCluster{},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, a)
	return a
}

// TestNewApplyReportsAnUnknownDistro is what the error return is for. The constructor used to
// return a nil *Apply for a distro ID it could not resolve, which every caller then dereferenced:
// the CLI reached it through a flag default and would have panicked, and the OpenTofu provider
// reaches it through an attribute an operator types.
func TestNewApplyReportsAnUnknownDistro(t *testing.T) {
	a, err := NewApply(ApplyOptions{
		Manager: &phase.Manager{
			DistroID: "k3ss",
			Config:   &cluster.ZarfCluster{},
		},
	})
	require.Error(t, err)
	require.Nil(t, a)
	require.Contains(t, err.Error(), "k3ss")
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
	a, err := NewApply(ApplyOptions{
		Manager: &phase.Manager{
			DistroID: "k3s",
			Config:   &cluster.ZarfCluster{},
		},
		AllowDowngrade: true,
	})
	require.NoError(t, err)
	require.NotNil(t, a)

	for _, p := range a.Phases {
		if facts, ok := p.(*phase.GatherFactsDistro); ok {
			require.True(t, facts.AllowDowngrade)
			return
		}
	}
	t.Fatal("apply no longer gathers distro facts")
}

// TestNewRefreshIsEntirelyReadOnly is the property the whole action rests on. A refresh runs
// against a live fleet during a plan, so a phase in the list that is not read-only would change a
// host while reporting on it -- and the classification the manager gates on is the same one this
// reads, so there is one answer rather than two.
func TestNewRefreshIsEntirelyReadOnly(t *testing.T) {
	r, err := NewRefresh(RefreshOptions{
		Manager: &phase.Manager{
			DistroID: "k3s",
			Config:   &cluster.ZarfCluster{},
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, r.Phases)

	for _, p := range r.Phases {
		if _, ok := p.(*phase.Disconnect); ok {
			// Disconnect declares its own dry-run path rather than being read-only, because in
			// an apply it has a temporary binary to remove. After a refresh there is nothing to
			// remove -- no phase in this list uploads anything -- so what it does here is close
			// the connections, which is the opposite of leaving something behind.
			require.NotEqual(t, phase.DryRunSkip, phase.ClassifyDryRun(p))
			continue
		}
		require.Equalf(
			t,
			phase.DryRunReadOnly,
			phase.ClassifyDryRun(p),
			"%s is in the refresh list but does not declare itself read-only", p.Title(),
		)
	}
}

// TestNewRefreshTakesNoLock pins the other half: Lock declares neither dry-run interface
// deliberately, so it could never be in the list above -- but a refresh must also not hold the
// cluster while it reads, because a plan is not a change and would otherwise block a real apply.
func TestNewRefreshTakesNoLock(t *testing.T) {
	r, err := NewRefresh(RefreshOptions{
		Manager: &phase.Manager{
			DistroID: "k3s",
			Config:   &cluster.ZarfCluster{},
		},
	})
	require.NoError(t, err)

	for _, p := range r.Phases {
		switch p.(type) {
		case *phase.Lock, *phase.Unlock:
			t.Errorf("%s takes the cluster lock during a read", p.Title())
		}
	}
}

// TestNewRefreshRequiresAManager covers the one input it cannot do without, since a nil manager
// would otherwise be dereferenced reading DistroID.
func TestNewRefreshRequiresAManager(t *testing.T) {
	r, err := NewRefresh(RefreshOptions{})
	require.Error(t, err)
	require.Nil(t, r)
}

// TestKubeConfigWritesUnlessToldNot holds what NoWrite means on each side. The zero value writes,
// because that is what every caller did before the field existed: `cargoship install kube-config`
// exists to write the file.
func TestKubeConfigWritesUnlessToldNot(t *testing.T) {
	for _, tc := range []struct {
		name      string
		noWrite   bool
		wantWrite bool
	}{
		{
			name:      "the zero value writes",
			noWrite:   false,
			wantWrite: true,
		},
		{
			name:      "NoWrite builds the credentials and writes nothing",
			noWrite:   true,
			wantWrite: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, err := NewKubeConfig(KubeConfigOptions{
				Manager: &phase.Manager{
					DistroID: "k3s",
					Config:   &cluster.ZarfCluster{},
				},
				NoWrite: tc.noWrite,
			})
			require.NoError(t, err)

			var found bool
			for _, p := range a.Phases {
				kubeconfig, ok := p.(*phase.KubeConfig)
				if !ok {
					continue
				}
				found = true
				require.Equal(t, tc.wantWrite, kubeconfig.Write)
			}
			require.True(t, found, "the action no longer holds a kubeconfig phase")
		})
	}
}

// TestKubeConfigBytesBeforeTheRun is the honest answer to being asked for credentials that have
// not been fetched: the sentinel, rather than an empty document a caller might write to disk.
func TestKubeConfigBytesBeforeTheRun(t *testing.T) {
	a, err := NewKubeConfig(KubeConfigOptions{
		Manager: &phase.Manager{
			DistroID: "k3s",
			Config:   &cluster.ZarfCluster{},
		},
		NoWrite: true,
	})
	require.NoError(t, err)

	_, err = a.Bytes()
	require.ErrorIs(t, err, phase.ErrNoKubeConfig)
	require.Nil(t, a.Config())
}

// TestApplyLabelsNodesWithoutWritingAKubeConfig covers a gate that was wrong rather than missing.
// LabelNodes used to be enabled only when UpdateKubeConfig was also set, which read as a
// dependency and is not one: the phase reads the cluster's admin credentials off a controller
// itself (see LabelNodes.clientset) and dials the load balancer with them, so the operator's own
// kubeconfig has nothing to do with it. Labelling a fleet meant writing a kubeconfig nobody asked
// for, and an OpenTofu apply has no business writing one at all.
func TestApplyLabelsNodesWithoutWritingAKubeConfig(t *testing.T) {
	a, err := NewApply(ApplyOptions{
		Manager: &phase.Manager{
			DistroID: "k3s",
			Config:   &cluster.ZarfCluster{},
		},
		LabelNodes:       true,
		UpdateKubeConfig: false,
	})
	require.NoError(t, err)

	var found bool
	for _, p := range a.Phases {
		label, ok := p.(*phase.LabelNodes)
		if !ok {
			continue
		}
		found = true
		require.True(t, label.Enabled, "label_nodes was asked for and the phase is disabled")
	}
	require.True(t, found, "apply no longer holds a label-nodes phase")
}
