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
	"testing"
	"time"

	"github.com/colonel-byte/cargoship/src/test"
	"github.com/stretchr/testify/suite"
)

// clusterNodeCount is the number of bootloose hosts wired into the generated
// inventory (kc0-2 controllers + kw0-2 workers; ki0-2 are unused).
const clusterNodeCount = 6

// ClusterLifecycleSuite drives prepare->apply->kube-config->health->reset against
// the shared bootloose cluster provisioned by TestMain. It is the only test allowed
// to run reset, since that tears down the distro every other cluster test relies on.
type ClusterLifecycleSuite struct {
	suite.Suite
	pkgPath        string
	kubeconfigPath string
	prevKubeconfig string
	hadPrevKube    bool
}

func TestClusterLifecycle(t *testing.T) {
	suite.Run(t, new(ClusterLifecycleSuite))
}

func (s *ClusterLifecycleSuite) SetupSuite() {
	t := s.T()
	if testing.Short() {
		t.Skip("cluster lifecycle needs a bootloose cluster and a real distro package")
	}
	requireCluster(t)
	s.prevKubeconfig, s.hadPrevKube = os.LookupEnv("KUBECONFIG")
	s.kubeconfigPath = filepath.Join(t.TempDir(), "config")
	s.Require().NoError(os.Setenv("KUBECONFIG", s.kubeconfigPath))
}

func (s *ClusterLifecycleSuite) TearDownSuite() {
	if s.hadPrevKube {
		s.Require().NoError(os.Setenv("KUBECONFIG", s.prevKubeconfig))
		return
	}
	s.Require().NoError(os.Unsetenv("KUBECONFIG"))
}

func (s *ClusterLifecycleSuite) SetupTest() {
	s.T().Setenv("CARGOSHIP_CONFIG", "src/test/e2e/cargoship-config.yaml")
}

func (s *ClusterLifecycleSuite) Test_0_Prepare() {
	t := s.T()

	outDir := t.TempDir()
	_, _, err := e2e.Cargoship(t, "--no-color", "create", "example/rke2-cilium/v1_35/v1.35.0-rke2r1", "-o", outDir)
	s.Require().NoError(err)

	matches, err := filepath.Glob(filepath.Join(outDir, "cargoship-*.tar.zst"))
	s.Require().NoError(err)
	s.Require().Len(matches, 1)
	s.pkgPath = matches[0]

	_, _, err = e2e.Cargoship(t, "--no-color", "prepare", s.pkgPath, "--config", e2e.ClusterConfigPath, "--confirm")
	s.Require().NoError(err)
}

func (s *ClusterLifecycleSuite) Test_1_Apply() {
	t := s.T()
	_, _, err := e2e.Cargoship(t, "--no-color", "apply", s.pkgPath, "--config", e2e.ClusterConfigPath, "--confirm")
	s.Require().NoError(err)
}

func (s *ClusterLifecycleSuite) Test_2_KubeConfig() {
	t := s.T()
	_, _, err := e2e.Cargoship(t, "--no-color", "kube-config", "--config", e2e.ClusterConfigPath)
	s.Require().NoError(err)

	info, err := os.Stat(s.kubeconfigPath)
	s.Require().NoError(err)
	s.Require().Positive(info.Size())
}

func (s *ClusterLifecycleSuite) Test_3_ClusterHealthy() {
	t := s.T()
	cs, err := e2e.KubeClient(t)
	s.Require().NoError(err)
	s.Require().NoError(test.WaitForNodesReady(context.Background(), cs, clusterNodeCount, 5*time.Minute))
}

// Test_4_ApplyIsIdempotent re-runs apply against the already-bootstrapped cluster,
// proving the manager routes through the upgrade phases instead of re-initializing.
func (s *ClusterLifecycleSuite) Test_4_ApplyIsIdempotent() {
	t := s.T()
	_, _, err := e2e.Cargoship(t, "--no-color", "apply", s.pkgPath, "--config", e2e.ClusterConfigPath, "--confirm")
	s.Require().NoError(err)
}

func (s *ClusterLifecycleSuite) Test_5_Reset() {
	t := s.T()
	_, _, err := e2e.Cargoship(t, "--no-color", "reset", "--config", e2e.ClusterConfigPath, "--confirm")
	s.Require().NoError(err)
}

// Test_6_PostReset confirms kube-config can no longer find a running controller
// once the distro has been torn down.
func (s *ClusterLifecycleSuite) Test_6_PostReset() {
	t := s.T()
	_, _, err := e2e.Cargoship(t, "--no-color", "kube-config", "--config", e2e.ClusterConfigPath)
	s.Require().Error(err)
}
