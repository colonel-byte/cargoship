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
	"os"
	"testing"
	"time"

	apicluster "github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/pkg/distro"
	"github.com/colonel-byte/cargoship/src/test"
)

// Node counts once the join walk's machine is in the inventory: one more Fedora worker than
// the apply walk saw, which is one more host, one more worker and one more node in the
// cluster. The upload-only count and the controller count do not move.
var (
	joinInventoryHostCount = inventoryHostCount + 1 //nolint:gochecknoglobals
	joinClusterNodeCount   = clusterNodeCount + 1   //nolint:gochecknoglobals
	joinClusterWorkers     = clusterWorkers + 1     //nolint:gochecknoglobals
)

// JoinPhaseSuite walks the apply phase list a second time, against a cluster that is already
// installed and one machine larger. It is how adding a node to a running cluster is tested:
// apply is the same command either way, so what the walk asserts is that each phase takes the
// new machine through what the install took the others through, and leaves the running nodes
// alone while it does.
//
// It runs after the apply walk, and the package it carries is the one that walk installed, at
// installedVersion. So the established nodes already run the packaged version, nothing routes
// to the upgrade phases, and the only host with work left is the new one.
//
// Most phases assert the same thing they assert on the install, and share their body with the
// apply walk through phaseWalk. The phases that differ are the ones that route on what is
// already running:
//
//   - Test_12 is where the difference starts. The established nodes report the installed
//     version and the new machine reports none, which is what sends only the new machine to
//     the initialize phases.
//   - Test_61 must claim nothing: the join adds a worker, and the control plane is up.
//   - Test_62 must claim the new machine and only the new machine.
//
// Two of the shared phases are worth reading as more than repetition. Test_25 and Test_26
// assert over the whole matrix of hosts, so they are what proves the established nodes learned
// about the new one -- an /etc/hosts entry and a firewall rule on every node, not just on the
// node that joined.
//
// The lock phases are left out: they are asserted in the apply walk, and holding a lock across
// a second walk against the same hosts tests the lock file rather than the join.
type JoinPhaseSuite struct {
	phaseWalk
	// joined is the host the walk added, resolved from the inventory in Test_05.
	joined *apicluster.ZarfHost
}

func (s *JoinPhaseSuite) SetupSuite() {
	t := s.T()
	if testing.Short() {
		t.Skip("the join phases need a bootloose cluster with the distro already installed")
	}
	requireCluster(t)

	ctx, err := phaseCtx(context.Background())
	s.Require().NoError(err)
	s.ctx = ctx
	s.pkgDir = t.TempDir()

	config.CLIArch = e2e.Arch
	config.CommonOptions.TempDirectory = os.TempDir()
}

func (s *JoinPhaseSuite) TearDownSuite() {
	if s.harness != nil {
		s.NoError(s.harness.close(s.ctx))
	}
}

// Test_00_JoinMachine starts the machine the walk joins and rewrites both inventories to name
// it. It also builds the package again: the walk installs the same version the cluster already
// runs, because a join is not an upgrade.
func (s *JoinPhaseSuite) Test_00_JoinMachine() {
	t := s.T()

	requireJoinMachine(t)

	cache, err := cachePath()
	s.Require().NoError(err)

	definition, err := containerSafeDefinition(examplePackage, s.pkgDir)
	s.Require().NoError(err)

	pkgPath, err := distro.Create(s.ctx, definition, s.pkgDir, distro.CreateOptions{
		Architecture: config.CLIArch,
		CachePath:    cache,
	})
	s.Require().NoError(err)
	s.Require().FileExists(pkgPath)
	s.pkgPath = pkgPath
}

// Test_05_Manager builds the manager the rest of the walk steps through, from the inventory
// Test_00 rewrote. The counts are the assertion that the new machine is in it: a walk built
// from a stale inventory would pass every later phase without ever touching the node it is
// supposed to be joining.
func (s *JoinPhaseSuite) Test_05_Manager() {
	harness, err := newPhaseHarness(s.ctx, s.pkgPath, fullClusterConfigPath, phaseHarnessOptions{
		Concurrency:      applyConcurrency,
		ModifyHosts:      true,
		ModifyFirewall:   true,
		UpdateKubeConfig: true,
		LabelNodes:       true,
		WorkerConcurrent: applyWorkerConcurrent,
		Timeout:          applyTimeout,
	})
	s.Require().NoError(err)
	s.harness = harness

	s.Require().Equal(distroID, s.harness.manager.DistroID)
	s.Require().Equal(installedVersion, s.harness.manager.Distro.Spec.Version,
		"the join walk is meant to install the version the cluster already runs")
	s.Require().Len(s.harness.hosts(), joinInventoryHostCount)
	s.Require().Len(s.harness.controllers(), clusterControllers)
	s.Require().Len(s.harness.engineWorkers(), joinClusterWorkers)
	s.Require().Len(s.harness.uploadOnly(), uploadOnlyCount)

	s.joined = s.harness.hosts().Find(func(h *apicluster.ZarfHost) bool {
		return h.Hostname == joinHostname
	})
	s.Require().NotNilf(s.joined, "the inventory does not name %s, the machine this walk joins", joinHostname)
}

// isJoined reports whether host is the machine this walk added.
func (s *JoinPhaseSuite) isJoined(host *apicluster.ZarfHost) bool {
	return host.Hostname == joinHostname
}

// Test_ZZ1_ClusterHealthy waits for the joined node to report Ready alongside the nodes the
// install bootstrapped. Test_62 already waited for the node it started, so this is the check
// that the cluster is one node larger rather than one node different.
func (s *JoinPhaseSuite) Test_ZZ1_ClusterHealthy() {
	t := s.T()
	cs, err := e2e.KubeClient(t)
	s.Require().NoError(err)
	s.Require().NoError(test.WaitForNodesReady(context.Background(), cs, joinClusterNodeCount, 5*time.Minute))
}
