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

	"github.com/colonel-byte/cargoship/src/pkg/phase"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// nodeRoleLabel is the label prefix phase/81_label_nodes.go adds each host's profile under.
const nodeRoleLabel = "node-role.kubernetes.io/"

// labelNodes covers phase/81_label_nodes.go. It reads each host's profile from the
// inventory and marks the matching Kubernetes node with it, so the assertion goes through
// the API server: every host that declares a profile has a node carrying that label set to
// "true". The generated inventory gives each host a profile matching its role, so this is
// the controller/worker split as the cluster sees it.
func (s *phaseWalk) labelNodes() {
	s.T().Helper()

	p := &phase.LabelNodes{Enabled: s.harness.opts.UpdateKubeConfig && s.harness.opts.LabelNodes}
	s.runPhase(p)
	s.Require().Equal(s.harness.opts.UpdateKubeConfig && s.harness.opts.LabelNodes, ran(p),
		"phase did not follow its enabled flag")

	if !ran(p) {
		return
	}

	cs, err := e2e.KubeClient(s.T())
	s.Require().NoError(err)

	nodes, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	s.Require().NoError(err)

	labels := make(map[string]map[string]string, len(nodes.Items))
	for _, node := range nodes.Items {
		labels[strings.ToLower(node.Name)] = node.Labels
	}

	for _, host := range s.harness.hosts() {
		if host.Profile == "" {
			continue
		}
		name := strings.ToLower(host.Metadata.Hostname)
		s.Require().Containsf(labels, name, "%s: no node registered as %q", host, name)
		s.Require().Equalf("true", labels[name][nodeRoleLabel+host.Profile],
			"%s: node is not labelled %s", host, nodeRoleLabel+host.Profile)
	}
}

// Test_81_LabelNodes marks each node with the profile the inventory gave its host.
func (s *ApplyPhaseSuite) Test_81_LabelNodes() {
	s.requireEngine()
	s.labelNodes()
}
