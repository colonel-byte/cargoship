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
	"strings"

	apicluster "github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/pkg/phase"
	"github.com/stretchr/testify/suite"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// phaseWalk is what ApplyPhaseSuite, JoinPhaseSuite and UpgradePhaseSuite have in common: one
// harness, one context, and the rule that the first failure stops the rest. Each walks the same
// phase list against the same cluster, one phase at a time, asserting what each phase left on
// the hosts.
//
// It also carries the assertions the walks share. A phase whose observable result is the same
// whether it is installing, joining a node or upgrading -- the upload phases, the engine
// config, the kubeconfig -- has its body here as an unexported method, and each suite's Test_NN
// method is a one-line call to it. Testify only collects methods whose name begins with "Test",
// so a method here is shared rather than run once per suite that embeds it.
//
// Where the walks differ, they differ for a reason worth reading -- the version the hosts
// report, which of the initialize and upgrade phases claims them -- and those assertions stay
// in the suite they belong to.
type phaseWalk struct {
	suite.Suite
	harness *phaseHarness
	ctx     context.Context
	pkgPath string
	// pkgDir is where the walk's package is created. It is taken in SetupSuite rather than in
	// the step that creates the package, because a TempDir taken inside a suite method is
	// removed when that method's subtest ends -- which is before the manager step that loads
	// the package runs, and the failure it produces names the package, not the directory.
	pkgDir string
	// phaseFailed short-circuits the remaining steps once one fails. Without it a single
	// broken phase reports as twenty failures and buries the one that matters.
	phaseFailed bool
}

func (s *phaseWalk) SetupTest() {
	if s.phaseFailed {
		s.T().Skip("skipping: an earlier step in this walk failed")
	}
	s.T().Setenv("CARGOSHIP_CONFIG", "src/test/e2e/cargoship-config.yaml")
}

func (s *phaseWalk) TearDownTest() {
	if s.T().Failed() {
		s.phaseFailed = true
	}
}

// runPhase executes one phase and fails the current test if it errors.
func (s *phaseWalk) runPhase(p phase.Phase) {
	s.T().Helper()
	s.Require().NoError(s.harness.run(s.ctx, p), "phase %q failed", p.Title())
}

// requireSchedulable asserts that each of the given hosts has a registered node the scheduler
// will still place pods on. It is what the upgrade phases have to leave behind: they drain and
// cordon a node before replacing the engine on it, and a node left cordoned is the failure
// that a running service and a correct version both look fine next to.
func (s *phaseWalk) requireSchedulable(hosts apicluster.ZarfHosts) {
	s.T().Helper()

	cs, err := e2e.KubeClient(s.T())
	s.Require().NoError(err)

	nodes, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	s.Require().NoError(err)

	unschedulable := make(map[string]bool, len(nodes.Items))
	for _, node := range nodes.Items {
		unschedulable[strings.ToLower(node.Name)] = node.Spec.Unschedulable
	}

	for _, host := range hosts {
		name := strings.ToLower(host.Metadata.Hostname)
		s.Require().Containsf(unschedulable, name, "%s: no node registered as %q", host, name)
		s.Require().Falsef(unschedulable[name], "%s: left cordoned after the upgrade", host)
	}
}
