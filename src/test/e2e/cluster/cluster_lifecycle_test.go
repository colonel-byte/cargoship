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
	"path/filepath"
	"time"

	"github.com/colonel-byte/cargoship/src/test"
)

// This file holds the steps of ApplyPhaseSuite that are not apply phases: building the
// package the phases install, the CLI-driven prepare that runs ahead of apply, and the
// checks that close the run out once every phase has been asserted. The phases themselves
// live in the numbered files mirroring src/pkg/phase, and take their method number from
// that file. These steps have no phase number to take, so they use the two ends of the
// ordering: Test_00 and Test_01 sort before every phase, and Test_ZZ1 onwards sort after
// every phase, because a letter sorts after a digit.

// Test_00_CreatePackage builds the distro package every later step installs.
func (s *ApplyPhaseSuite) Test_00_CreatePackage() {
	t := s.T()

	outDir := t.TempDir()
	_, _, err := e2e.Cargoship(t, "--no-color", "create", "example/rke2-cilium/v1_35/v1.35.0-rke2r1", "-o", outDir)
	s.Require().NoError(err)

	matches, err := filepath.Glob(filepath.Join(outDir, "cargoship-*.tar.zst"))
	s.Require().NoError(err)
	s.Require().Len(matches, 1)
	s.pkgPath = matches[0]
}

// Test_01_Prepare runs the prepare command, the separate phase list that readies the hosts
// before apply. It is driven through the CLI because it is its own action, not part of the
// apply order this suite walks.
func (s *ApplyPhaseSuite) Test_01_Prepare() {
	t := s.T()
	_, _, err := e2e.Cargoship(t, "--no-color", "prepare", s.pkgPath, "--config", e2e.ClusterConfigPath, "--confirm")
	s.Require().NoError(err)
}

// Test_ZZ1_ClusterHealthy waits for every node the inventory named to report Ready, proving
// the phases the suite just walked produced a working cluster and not only the right files.
func (s *ApplyPhaseSuite) Test_ZZ1_ClusterHealthy() {
	t := s.T()
	cs, err := e2e.KubeClient(t)
	s.Require().NoError(err)
	s.Require().NoError(test.WaitForNodesReady(context.Background(), cs, clusterNodeCount, 5*time.Minute))
}

// Test_ZZ2_ApplyIsIdempotent re-runs the whole apply through the CLI against the
// already-bootstrapped cluster, proving the manager routes through the upgrade phases
// instead of re-initializing.
func (s *ApplyPhaseSuite) Test_ZZ2_ApplyIsIdempotent() {
	t := s.T()
	_, _, err := e2e.Cargoship(t, "--no-color", "apply", s.pkgPath, "--config", e2e.ClusterConfigPath, "--confirm")
	s.Require().NoError(err)
}

// Test_ZZ3_Reset tears the distro back off the nodes. It is the last destructive step, and
// the only place reset runs, since every other cluster test depends on the install.
func (s *ApplyPhaseSuite) Test_ZZ3_Reset() {
	t := s.T()
	_, _, err := e2e.Cargoship(t, "--no-color", "reset", "--config", e2e.ClusterConfigPath, "--confirm")
	s.Require().NoError(err)
}

// Test_ZZ4_PostReset confirms kube-config can no longer find a running controller once the
// distro has been torn down.
func (s *ApplyPhaseSuite) Test_ZZ4_PostReset() {
	t := s.T()
	_, _, err := e2e.Cargoship(t, "--no-color", "kube-config", "--config", e2e.ClusterConfigPath)
	s.Require().Error(err)

	_, err = os.Stat(s.kubeconfigPath)
	s.Require().NoError(err)
}
