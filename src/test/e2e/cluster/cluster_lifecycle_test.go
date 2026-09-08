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
	"time"

	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/pkg/action"
	"github.com/colonel-byte/cargoship/src/pkg/distro"
	"github.com/colonel-byte/cargoship/src/pkg/phase"
	"github.com/colonel-byte/cargoship/src/test"
)

// This file holds the steps of ApplyPhaseSuite that are not apply phases: building the
// package the phases install, the prepare that runs ahead of apply, and the checks that close
// the run out once every phase has been asserted. The phases themselves live in the numbered
// files mirroring src/pkg/phase, and take their method number from that file. These steps have
// no phase number to take, so they use the two ends of the ordering: Test_00 and Test_01 sort
// before every phase, and Test_ZZ1 onwards sort after every phase, because a letter sorts
// after a digit.
//
// Every step here calls the same package the matching CLI command calls -- distro.Create for
// create, action.NewPrepare for prepare, and so on -- rather than shelling out to a built
// binary, so the suite runs from a bare checkout with nothing under build/. Each one builds
// its own manager and removes its own temp directory, the way a separate CLI process would.

// examplePackage is the distro definition the suite packages and installs, and
// installedVersion is the engine version it ships. The version is spelled out rather than read
// back from the package, so that the manager step compares the loaded package against
// something that did not come from the same file it is testing.
const (
	examplePackage   = "example/rke2-cilium/v1_35/v1.35.0-rke2r1"
	installedVersion = "1.35.0-rke2r1"
)

// Test_00_CreatePackage builds the distro package every later step installs. It builds it
// from a copy of the example definition with the sysctls a container cannot apply removed:
// see containerSafeDefinition.
func (s *ApplyPhaseSuite) Test_00_CreatePackage() {
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

// Test_01_Prepare runs the prepare action, the separate phase list that readies the hosts
// before apply. It runs whole rather than phase by phase because it is its own action, not
// part of the apply order this suite walks.
func (s *ApplyPhaseSuite) Test_01_Prepare() {
	manager, cleanup := s.newManager(e2e.ClusterConfigPath)
	defer cleanup()

	err := action.NewPrepare(action.PrepareOptions{
		Manager:        manager,
		ModifyHosts:    true,
		ModifyFirewall: true,
		ModifyModules:  true,
	}).Run(s.ctx)
	s.Require().NoError(err)
}

// Test_ZZ1_ClusterHealthy waits for every node the inventory named to report Ready, proving
// the phases the suite just walked produced a working cluster and not only the right files.
func (s *ApplyPhaseSuite) Test_ZZ1_ClusterHealthy() {
	s.requireEngine()
	t := s.T()
	cs, err := e2e.KubeClient(t)
	s.Require().NoError(err)
	s.Require().NoError(test.WaitForNodesReady(context.Background(), cs, clusterNodeCount, 5*time.Minute))
}

// Test_ZZ2_ApplyIsIdempotent re-runs the whole apply against the already-bootstrapped
// cluster, proving the manager routes through the upgrade phases instead of re-initializing.
// It is also the only step that exercises action.NewApply's own wiring, since the phase
// tests build each phase themselves.
func (s *ApplyPhaseSuite) Test_ZZ2_ApplyIsIdempotent() {
	s.requireEngine()
	manager, cleanup := s.newManager(e2e.ClusterConfigPath)
	defer cleanup()

	err := action.NewApply(action.ApplyOptions{
		Manager:          manager,
		ModifyHosts:      true,
		ModifyFirewall:   true,
		WorkerConcurrent: applyWorkerConcurrent,
		UpdateKubeConfig: true,
		LabelNodes:       true,
	}).Run(s.ctx)
	s.Require().NoError(err)
}

// newManager builds a package-loading manager for one of the actions above, along with the
// cleanup that removes the package it extracted. Each action gets its own, because each CLI
// command gets its own.
func (s *ApplyPhaseSuite) newManager(configPath string) (*phase.Manager, func()) {
	s.T().Helper()

	manager, _, err := newManager(s.ctx, s.pkgPath, configPath, applyConcurrency, applyTimeout)
	s.Require().NoError(err)

	return manager, func() { s.NoError(os.RemoveAll(manager.TempDirectory)) }
}
