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

	apicluster "github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/action"
	"github.com/colonel-byte/cargoship/pkg/distro"
	"github.com/colonel-byte/cargoship/pkg/phase"
	"github.com/colonel-byte/cargoship/test"
	"github.com/stretchr/testify/suite"
)

// dryRunPlannedFresh is how many phases an apply dry run reported over nodes with nothing on
// them. ApplyPhaseSuite reads it back after the install to assert the report is derived from
// what is on the hosts rather than being a static roster of the phase list. It is a package
// variable because the two walks are separate suites by necessity: one runs before the install
// and the other after it.
var dryRunPlannedFresh int //nolint:gochecknoglobals

// DryRunSuite is what PR #312's unit tests cannot cover. Those run fake phases through the
// manager and pin the gate: a phase declaring neither readOnly nor withDryRun is reported rather
// than run, hooks and CleanUp are skipped with it, and a skipped phase's Prepare failing does not
// fail the run. What they cannot assert is the property the flag actually sells, which is that a
// dry run against real hosts leaves them unchanged.
//
// So this walk runs first, before ApplyPhaseSuite installs anything, because "unchanged" is only
// checkable on a host nothing has been done to yet. Every assertion here is the negation of one
// the install walk makes: no staged files where 50_uploadfiles_test.go finds a manifest, no
// sysctl file where 20_prepare_host_test.go finds one, no lock file where 91_lock_test.go finds
// one, no running engine where 61_initialize_controller_test.go finds one.
//
// It also asserts the other half, which matters more than it looks: the read-only phases ran for
// real. A dry run that reported without connecting to anything would pass an untouched-hosts
// check trivially, so the facts the read-only phases gather have to be on the hosts afterwards.
//
// It runs whole actions rather than stepping phase by phase, because the dry-run gate is the
// manager's and the thing under test is what a whole run leaves behind.
type DryRunSuite struct {
	suite.Suite
	ctx    context.Context
	pkgDir string
	// pkgPath is the package the apply and prepare dry runs load. Reset loads none.
	pkgPath string
	// stepFailed short-circuits the later steps once one fails, the way phaseWalk does: a dry
	// run that errored has already told us what went wrong, and the untouched-hosts assertions
	// after it would each report the same failure again.
	stepFailed bool
}

func (s *DryRunSuite) SetupSuite() {
	t := s.T()
	if testing.Short() {
		t.Skip("the dry-run walk needs a bootloose cluster and a real distro package")
	}
	requireCluster(t)

	ctx, err := phaseCtx(context.Background())
	s.Require().NoError(err)
	s.ctx = ctx
	s.pkgDir = t.TempDir()

	config.CLIArch = e2e.Arch
	config.CommonOptions.TempDirectory = os.TempDir()
}

func (s *DryRunSuite) SetupTest() {
	if s.stepFailed {
		s.T().Skip("skipping: an earlier step in this walk failed")
	}
	s.T().Setenv("CARGOSHIP_CONFIG", "test/e2e/cargoship-config.yaml")
}

func (s *DryRunSuite) TearDownTest() {
	if s.T().Failed() {
		s.stepFailed = true
	}
}

// Test_1_CreatePackage builds the package the dry runs load. It is the same definition
// ApplyPhaseSuite installs, built into this suite's own directory, because a dry run of an apply
// has to load a package to have anything to report.
func (s *DryRunSuite) Test_1_CreatePackage() {
	cache, err := cachePath()
	s.Require().NoError(err)

	definition, err := test.ContainerSafeDefinition(examplePackage(), s.pkgDir)
	s.Require().NoError(err)

	pkgPath, err := distro.Create(s.ctx, definition, s.pkgDir, distro.CreateOptions{
		Architecture: config.CLIArch,
		CachePath:    cache,
	})
	s.Require().NoError(err)
	s.Require().FileExists(pkgPath)
	s.pkgPath = pkgPath
}

// Test_2_ApplyDryRunLeavesTheHostsAlone is the heart of this walk: the whole apply phase list,
// against nodes with nothing on them, with DryRun set. Afterwards the hosts carry no artifact any
// phase would have left, and the facts the read-only phases gathered are on them.
func (s *DryRunSuite) Test_2_ApplyDryRunLeavesTheHostsAlone() {
	manager, cleanup := s.newManager(fullClusterConfigPath)
	defer cleanup()

	ctx, sink := phase.WithResultSink(s.ctx)
	apply, err := action.NewApply(action.ApplyOptions{
		Manager:          manager,
		ModifyHosts:      true,
		ModifyFirewall:   true,
		WorkerConcurrent: applyWorkerConcurrent,
		UpdateKubeConfig: true,
		LabelNodes:       true,
	})
	s.Require().NoError(err)
	s.Require().NoError(apply.Run(ctx))

	result := sink.Result()
	s.Require().True(sink.Observed(), "the run never reached its phases")
	s.requireNothingRanThatWrites(result)

	hosts := manager.Config.Spec.Hosts
	s.Require().Len(hosts, inventoryHostCount)
	// The facts are read off the host objects the run filled in, so they survive the disconnect
	// the run ends with. Looking at the hosts themselves does not, which is what reconnect is for.
	s.requireFactsGathered(hosts)

	s.reconnect(manager)
	defer disconnectAll(hosts)
	s.requireHostsUntouched(hosts)

	dryRunPlannedFresh = len(result.Planned)
	s.Require().NotZero(dryRunPlannedFresh, "a dry run over fresh nodes reported no phases at all")
}

// Test_3_PrepareDryRunLeavesTheHostsAlone covers the other command that takes the flag and
// changes hosts. Prepare writes the environment, the sysctl settings and the kernel modules, so
// the assertion is that none of them arrived.
func (s *DryRunSuite) Test_3_PrepareDryRunLeavesTheHostsAlone() {
	manager, cleanup := s.newManager(fullClusterConfigPath)
	defer cleanup()

	err := action.NewPrepare(action.PrepareOptions{
		Manager:        manager,
		ModifyHosts:    true,
		ModifyFirewall: true,
		ModifyModules:  true,
	}).Run(s.ctx)
	s.Require().NoError(err)

	s.reconnect(manager)
	defer disconnectAll(manager.Config.Spec.Hosts)
	s.requireHostsUntouched(manager.Config.Spec.Hosts)
}

// Test_4_ResetDryRunWithNoRunningController is the case a live cluster found rather than a test,
// in PR #312: the delete phases find their leader by looking for a host already running the
// controller service, and there is none here, so Prepare reaches a nil leader. It has to report
// and exit rather than panic.
//
// Nodes with nothing installed are the natural fixture for it, which is why it runs in this walk
// instead of after the reset. The already-reset cluster is the same state arrived at the long way
// round.
func (s *DryRunSuite) Test_4_ResetDryRunWithNoRunningController() {
	manager, err := newBareManager(s.ctx, e2e.ClusterConfigPath, distroID(), applyConcurrency)
	s.Require().NoError(err)
	manager.DryRun = true

	reset, err := action.NewReset(action.ResetOptions{
		Manager:          manager,
		WorkerConcurrent: applyWorkerConcurrent,
		NoWait:           true,
		NoDrain:          true,
	})
	s.Require().NoError(err)
	s.Require().NoError(reset.Run(s.ctx))

	s.reconnect(manager)
	defer disconnectAll(manager.Config.Spec.Hosts)
	s.requireHostsUntouched(manager.Config.Spec.Hosts)
}

// newManager builds a package-loading manager with the dry-run flag set, and the cleanup that
// removes the package it extracted.
func (s *DryRunSuite) newManager(configPath string) (*phase.Manager, func()) {
	s.T().Helper()

	manager, _, err := newManager(s.ctx, s.pkgPath, configPath, applyConcurrency, applyTimeout)
	s.Require().NoError(err)
	manager.DryRun = true

	return manager, func() { s.NoError(os.RemoveAll(manager.TempDirectory)) }
}

// reconnect reopens the connections the run closed, so the assertions below read the host rather
// than a closed connection. See reconnectHosts.
func (s *DryRunSuite) reconnect(manager *phase.Manager) {
	s.T().Helper()
	s.Require().NoError(reconnectHosts(s.ctx, manager), "could not reconnect to the hosts to assert on them")
}

// requireNothingRanThatWrites asserts the gate held: every phase that executed was one a dry run
// is allowed to execute, and every phase that was reported instead was one that declared nothing.
//
// Asserting the classification rather than a list of titles is deliberate. A phase added later
// shows up here as soon as it runs under a dry run without declaring why that is safe, which is
// the regression this is for; pinning titles would only notice the phases that exist today.
func (s *DryRunSuite) requireNothingRanThatWrites(result phase.RunResult) {
	s.T().Helper()

	for _, p := range result.Ran {
		s.Require().NotEqualf(phase.DryRunSkip, p.DryRun,
			"%q ran under a dry run without declaring readOnly or a DryRun path", p.Title)
	}
	for _, p := range result.Planned {
		s.Require().Equalf(phase.DryRunSkip, p.DryRun,
			"%q was reported rather than run, but declares that it is safe to run", p.Title)
	}
}

// requireFactsGathered asserts the read-only phases did their work for real. Without this, a dry
// run that connected to nothing would satisfy requireHostsUntouched.
func (s *DryRunSuite) requireFactsGathered(hosts apicluster.ZarfHosts) {
	s.T().Helper()

	for _, host := range hosts {
		s.Require().NotNilf(host.Configurer, "%s: OS was never detected", host)
		s.Require().NotEmptyf(host.Metadata.Hostname, "%s: no hostname was gathered", host)
		s.Require().NotEmptyf(host.Metadata.Arch, "%s: no architecture was gathered", host)
		// Nothing is installed yet, so the distro facts phase records the sentinel rather than a
		// version. That it is set at all is what says the phase ran.
		s.Require().Equalf(phase.UnknownVersion, host.Metadata.DistroVersion,
			"%s: reported an engine version on a host nothing has been installed on", host)
	}
}

// requireHostsUntouched asserts that none of the artifacts the install walk looks for is present.
// Each path is the one the matching install test asserts exists, so the two move together.
func (s *DryRunSuite) requireHostsUntouched(hosts apicluster.ZarfHosts) {
	s.T().Helper()

	service := distroControllerService(s.T())

	for _, host := range hosts {
		s.Require().Falsef(host.FileExist(uploadManifestPath),
			"%s: a dry run staged files and wrote %s", host, uploadManifestPath)
		s.Require().Falsef(host.FileExist(sysctlConfPath),
			"%s: a dry run wrote %s", host, sysctlConfPath)

		// The lock is the one phase in the apply list that deliberately declares neither
		// interface, so a dry run must leave no lock file behind. TestLockHasNoDryRunPath pins
		// the interface; this pins the host.
		if host.Configurer != nil {
			lockPath := host.Configurer.CTLLockFilePath(host)
			s.Require().Falsef(host.FileExist(lockPath),
				"%s: a dry run took the lock and left %s", host, lockPath)
		}

		s.Require().Falsef(host.ServiceIsRunning(s.ctx, service),
			"%s: a dry run started %s", host, service)
	}
}

// distroControllerService is the control-plane service name for the distro under test, read from
// the distro module the way every phase reads it.
func distroControllerService(t *testing.T) string {
	t.Helper()

	dis, err := distroModule(distroID())
	if err != nil {
		t.Fatalf("no distro module for %q: %v", distroID(), err)
	}
	return dis.GetControllerService()
}
