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

	"github.com/colonel-byte/cargoship/src/config"
)

// distroID is the distro the suite installs, and the value the CLI's --distro flag would
// carry. The steps that build a manager with no package loaded need it spelled out, because
// there is no package for them to read it from.
const distroID = "rke2"

// Node counts for the generated inventory, see the bootloose config in main_test.go: kc0,
// kc1, kcf0 are controllers and kw0-2, kwf0-2, kwa0 are workers, with the "f" replicas
// running Fedora, the "a" replica Alpine and the rest Ubuntu.
//
// The Alpine host is a worker in the inventory, because that is what the upload phases key
// on, but it never joins the cluster. So there are two counts: everything the upload phases
// see, and everything that runs the engine.
//
// They are read off whichever bootloose config this run provisions rather than written down,
// because a stage-only run provisions the smaller one. See clusterConfig.
var (
	applyCounts        = countsFor(clusterConfig())                     //nolint:gochecknoglobals
	inventoryHostCount = applyCounts.inventory                          //nolint:gochecknoglobals
	uploadOnlyCount    = applyCounts.uploadOnly                         //nolint:gochecknoglobals
	clusterNodeCount   = applyCounts.inventory - applyCounts.uploadOnly //nolint:gochecknoglobals
	clusterControllers = applyCounts.controllers                        //nolint:gochecknoglobals
	clusterWorkers     = applyCounts.workers                            //nolint:gochecknoglobals
)

// applyTimeout bounds the retry loops the phases run through manager.RetryTimeout, and
// applyConcurrency and applyWorkerConcurrent are the widths the phases act on the cluster
// at. All three are shared with the actions in cluster_lifecycle_test.go, so a phase run one
// at a time and the same phase run inside a full apply see identical settings.
const (
	applyTimeout          = 20 * time.Minute
	applyConcurrency      = 300
	applyWorkerConcurrent = "5"
)

// ApplyPhaseSuite walks the apply phase list one phase at a time against the shared
// bootloose cluster, asserting the artifact each phase leaves on the hosts before the next
// phase runs. It replaces a single `cargoship apply` invocation: running both would install
// the distro twice on the same cluster for no extra coverage.
//
// Layout: each phase's assertions live in the file matching its source file in
// src/pkg/phase, and the method carries that same number, so
// 25_modify_hosts_file_test.go covers phase/25_modify_hosts_file.go in Test_25_ModifyHosts.
// One number per phase, in the file name and the method name, is what makes the test for a
// phase findable from the phase.
//
// Testify runs suite methods in method-name order, so that number is also the order the
// phases run in here. It matches the order apply runs them in everywhere except the lock:
// apply takes the lock third, right after OS detection, and holds it for the whole run,
// whereas phase/91_lock.go's number puts it after the install. The suite still asserts what
// the phase leaves on the hosts, it just no longer holds the lock across the phases in
// between. Every other phase is in the same relative position apply puts it in.
//
// Steps that are not phases have no phase number to borrow: package create and prepare run
// first as Test_00 and Test_01, and the health and idempotency checks run last as Test_ZZ1
// and Test_ZZ2, "ZZ" sorting after every two-digit number. They live in
// cluster_lifecycle_test.go.
//
// Every phase test shares one phaseHarness, so they are ordered and stateful by design.
// Running a single phase with -run will fail: its predecessors never connected the hosts.
//
// Not every host in the inventory joins the cluster. The Alpine host is there so that the BIN
// upload phase is tested on a host belonging to neither OS family the other upload phases
// claim, and Test_60_ConfigureEngine drops it before the first phase that touches the engine.
// So the phases up to and including Test_59 see every host in the inventory and everything
// after them sees one fewer.
//
// That same boundary is where a stage-only run stops asserting: the methods from Test_61 to
// Test_81 begin with requireEngine, which skips them when stageOnlyEnvVar is set. The walk still
// runs to the end. Test_91, Test_92 and Test_99 are deliberately not gated -- the lock is a file
// on each host, unlock removes it, and disconnect drops the SSH connections, none of which needs
// an engine -- so a stage-only run still takes the lock, releases it and disconnects cleanly, and
// still fails if any of that regresses. See stageOnlyEnvVar for why the halves are separated.
//
// The suite is one of two walks TestClusterPhases runs against the same cluster, in the only
// order they work in: this one installs, and JoinPhaseSuite adds a machine to what it
// installed. A stage-only run installs nothing, so it runs this walk alone.
type ApplyPhaseSuite struct {
	phaseWalk
}

func (s *ApplyPhaseSuite) SetupSuite() {
	t := s.T()
	if testing.Short() {
		t.Skip("apply phases need a bootloose cluster and a real distro package")
	}
	requireCluster(t)

	ctx, err := phaseCtx(context.Background())
	s.Require().NoError(err)
	s.ctx = ctx
	s.pkgDir = t.TempDir()

	config.CLIArch = e2e.Arch
	config.CommonOptions.TempDirectory = os.TempDir()
}

func (s *ApplyPhaseSuite) TearDownSuite() {
	if s.harness != nil {
		s.NoError(s.harness.close(s.ctx))
	}
}

// Test_05_Manager builds the manager the phase tests step through, from the same inputs
// `cargoship apply` builds its own from: the generated bootloose inventory and the package
// created in Test_00. It uses the full inventory rather than the one the CLI steps are given,
// because the upload phases are meant to see the upload-only host.
func (s *ApplyPhaseSuite) Test_05_Manager() {
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
	s.Require().Equal(installedVersion, s.harness.manager.Distro.Spec.Version)
	s.Require().Len(s.harness.hosts(), inventoryHostCount)
	s.Require().Len(s.harness.controllers(), clusterControllers)
	s.Require().Len(s.harness.engineWorkers(), clusterWorkers)
	s.Require().Len(s.harness.uploadOnly(), uploadOnlyCount)
	s.Require().DirExists(s.harness.manager.TempDirectory)
}
