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
	"fmt"

	"github.com/colonel-byte/cargoship/src/pkg/phase"
	"k8s.io/client-go/tools/clientcmd"
)

// kubeConfig covers phase/80_kubeconfig.go. It writes the admin credentials for the
// cluster into the local kubeconfig, so the assertion is on the file this test binary points
// KUBECONFIG at: it names the cluster, points at the load balancer address the inventory
// declared, and carries the client certificate. Test_ZZ1 then uses that same file to talk to
// the cluster, which is the real proof the credentials work.
//
// Both walks assert the same thing here, so the body is shared: see phaseWalk.
func (s *phaseWalk) kubeConfig() {
	s.T().Helper()

	clusterID := s.harness.manager.Config.Metadata.Name

	p := &phase.KubeConfig{
		Distro:    s.harness.distro,
		ClusterID: clusterID,
		Enabled:   s.harness.opts.UpdateKubeConfig,
	}
	s.runPhase(p)
	s.Require().Equal(s.harness.opts.UpdateKubeConfig, ran(p), "phase did not follow its enabled flag")

	if !ran(p) {
		return
	}

	cfg, err := clientcmd.LoadFromFile(kubeconfigPath)
	s.Require().NoError(err)

	s.Require().Equal(clusterID, cfg.CurrentContext)
	s.Require().Contains(cfg.Clusters, clusterID)
	s.Require().Equal(
		fmt.Sprintf("https://%s:6443", s.harness.manager.Config.Spec.Config.LoadBalancer),
		cfg.Clusters[clusterID].Server,
	)
	s.Require().NotEmpty(cfg.Clusters[clusterID].CertificateAuthorityData)

	admin := fmt.Sprintf("%s-admin", clusterID)
	s.Require().Contains(cfg.AuthInfos, admin)
	s.Require().NotEmpty(cfg.AuthInfos[admin].ClientCertificateData)
	s.Require().NotEmpty(cfg.AuthInfos[admin].ClientKeyData)
}

// Test_80_KubeConfig writes the admin credentials for the cluster the install created.
func (s *ApplyPhaseSuite) Test_80_KubeConfig() {
	s.kubeConfig()
}

// Test_80_KubeConfig rewrites the credentials against the upgraded control plane, proving the
// upgrade did not leave the local kubeconfig pointing at something that no longer answers.
func (s *UpgradePhaseSuite) Test_80_KubeConfig() {
	s.kubeConfig()
}

// Test_80_KubeConfig rewrites the credentials once the cluster has grown, proving the join did
// not leave the local kubeconfig pointing at something that no longer answers.
func (s *JoinPhaseSuite) Test_80_KubeConfig() {
	s.kubeConfig()
}
