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

	"github.com/colonel-byte/cargoship/src/pkg/phase"
	"github.com/stretchr/testify/suite"
)

// phaseWalk is what a walk over the phase list is made of: one harness, one context, and the
// rule that the first failure stops the rest. A walk steps through the phases against the
// cluster one at a time, asserting what each phase left on the hosts. ApplyPhaseSuite embeds
// it and is the walk that installs the distro.
//
// It also carries the assertions themselves. A phase whose observable result does not depend
// on what the walk is doing to the cluster has its body here as an unexported method, and the
// suite's Test_NN method is a one-line call to it. Testify only collects methods whose name
// begins with "Test", so a method here is shared rather than run once per suite that embeds
// it. Assertions that hold only for one walk stay in the suite they belong to.
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

// requireEngine skips a step that installs, starts or queries the engine, on a run that was
// asked for the staging phases only. See stageOnly.
func (s *phaseWalk) requireEngine() {
	s.T().Helper()
	if stageOnly() {
		s.T().Skip("stage-only run: this step needs a started engine")
	}
}

// runPhase executes one phase and fails the current test if it errors.
func (s *phaseWalk) runPhase(p phase.Phase) {
	s.T().Helper()
	s.Require().NoError(s.harness.run(s.ctx, p), "phase %q failed", p.Title())
}
