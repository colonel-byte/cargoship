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
	"strconv"
	"testing"
	"time"

	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/pkg/distro"
	"github.com/colonel-byte/cargoship/src/pkg/phase"
	"github.com/colonel-byte/cargoship/src/test"
)

// exampleUpgradePackage is the definition the upgrade walk installs over what ApplyPhaseSuite
// left behind. It is the next patch release of the same minor version as examplePackage, and
// deliberately the smallest real upgrade the examples offer: the two differ in the engine
// tarball, three RPM URLs and the container image tags, and in nothing else. A larger jump
// would exercise the same phases while adding image pulls and a CNI version change on top.
const exampleUpgradePackage = "example/rke2-cilium/v1_35/v1.35.0-rke2r3"

// upgradeEnvVar opts the upgrade walk in. It is off by default because the upgrade installs a
// second complete set of engine images onto all nine nodes: it roughly doubles the disk the
// suite needs and adds tens of minutes, neither of which fits a hosted CI runner that is
// already close to full running the install alone.
const upgradeEnvVar = "CARGOSHIP_E2E_UPGRADE"

// upgradeEnabled reports whether the upgrade walk was asked for.
func upgradeEnabled() bool {
	on, err := strconv.ParseBool(os.Getenv(upgradeEnvVar))
	return err == nil && on
}

// UpgradePhaseSuite walks the phases again against the cluster ApplyPhaseSuite installed,
// this time with a newer package, and asserts what each phase leaves behind exactly as the
// apply walk does. It is what covers the half of the phase list a fresh install cannot reach:
// on a fresh install the upgrade phases claim no hosts and the config sync finds no drift, so
// until something upgrades a real cluster, phase/66 and phase/67 are only ever asserted to
// have done nothing.
//
// It starts from GatherFactsDistro rather than from the top of the list. Connect, DetectOS,
// GatherFacts and ValidateHosts do the same work against the same hosts they did during the
// install, and the apply walk already asserts them one at a time, so they run here as a
// single setup step whose only job is to hand the phases a connected inventory.
//
// The phases from GatherFactsDistro on are the ones whose behaviour actually differs, and
// they each get a method:
//
//   - Test_12 is where the difference starts. The hosts now report a real engine version, and
//     that it is older than the package is what routes everything after it.
//   - Test_61 and Test_62 must claim nothing. Apply runs the initialize and the upgrade phases
//     in one list and lets each Prepare pick its own hosts; an initialize phase that claimed a
//     running node here would re-bootstrap a live cluster.
//   - Test_66 and Test_67 are the mirror image, and the only place they are asserted to have
//     run at all.
//
// The lock phases are left out. They are asserted in the apply walk, and holding a lock over a
// second walk against the same hosts tests the lock file rather than the upgrade.
type UpgradePhaseSuite struct {
	phaseWalk
}

func (s *UpgradePhaseSuite) SetupSuite() {
	t := s.T()
	if testing.Short() {
		t.Skip("upgrade phases need a bootloose cluster with the distro already installed")
	}
	if !upgradeEnabled() {
		t.Skipf("set %s=1 to walk the upgrade phases as well as the install", upgradeEnvVar)
	}
	requireCluster(t)

	ctx, err := phaseCtx(context.Background())
	s.Require().NoError(err)
	s.ctx = ctx
	s.pkgDir = t.TempDir()

	config.CLIArch = e2e.Arch
	config.CommonOptions.TempDirectory = os.TempDir()
}

func (s *UpgradePhaseSuite) TearDownSuite() {
	if s.harness != nil {
		s.NoError(s.harness.close(s.ctx))
	}
}

// Test_00_UpgradePackage builds the newer package and the manager the rest of the walk steps
// through. The harness is built from the full inventory, the same one the apply walk used, so
// that the upload phases see the upload-only host again -- an upgrade has to replace what it
// staged there too, even though the host never runs the engine.
func (s *UpgradePhaseSuite) Test_00_UpgradePackage() {
	cache, err := cachePath()
	s.Require().NoError(err)

	definition, err := containerSafeDefinition(exampleUpgradePackage, s.pkgDir)
	s.Require().NoError(err)

	pkgPath, err := distro.Create(s.ctx, definition, s.pkgDir, distro.CreateOptions{
		Architecture: config.CLIArch,
		CachePath:    cache,
	})
	s.Require().NoError(err)
	s.Require().FileExists(pkgPath)
	s.pkgPath = pkgPath

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
	// The join walk ran before this one and left its machine in the inventory, so the count
	// the upgrade expects is the joined one: an upgrade that saw only the ten hosts the
	// install saw would leave the node that joined last on the old engine.
	s.Require().Len(s.harness.hosts(), joinInventoryHostCount)
	s.Require().NotEqual(installedVersion, s.harness.manager.Distro.Spec.Version,
		"the upgrade package ships the version the cluster already runs, so nothing would upgrade")
}

// Test_01_Reconnect runs the four phases that only re-establish what the install already
// proved: the SSH connections, the detected OS, the host facts and the validation. They are
// asserted one at a time in the apply walk, so here they run as one step and the assertion is
// only that the hosts came back.
func (s *UpgradePhaseSuite) Test_01_Reconnect() {
	s.runPhase(&phase.Connect{})
	s.runPhase(&phase.DetectOS{})
	s.runPhase(&phase.GatherFacts{})
	s.runPhase(&phase.ValidateHosts{})

	for _, host := range s.harness.hosts() {
		s.Require().NotNilf(host.Configurer, "%s: no configurer after the OS detection phase", host)
		s.Require().NotEmptyf(host.Metadata.Hostname, "%s: no hostname after the facts phase", host)
	}
}

// Test_ZZ1_ClusterHealthy waits for every node to report Ready again on the upgraded engine.
// The upgrade phases already waited for each node they touched, so this is the check that the
// cluster is whole afterwards rather than only node by node during.
func (s *UpgradePhaseSuite) Test_ZZ1_ClusterHealthy() {
	t := s.T()
	cs, err := e2e.KubeClient(t)
	s.Require().NoError(err)
	s.Require().NoError(test.WaitForNodesReady(context.Background(), cs, joinClusterNodeCount, 5*time.Minute))
}
