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

package phase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// nodeRoleLabelPrefix prefixes the Kubernetes node label cargoship uses to mark which
// profile group a node belongs to.
const nodeRoleLabelPrefix = "node-role.kubernetes.io/"

// LabelNodes phase state
type LabelNodes struct {
	GenericPhase
	ClusterLB string
	Enabled   bool
	leader    *cluster.ZarfHost
}

// Title for the phase
func (p *LabelNodes) Title() string {
	return "Labeling nodes with their profile group"
}

// Explanation about the current phase, used for documentation generation
func (p *LabelNodes) Explanation() string {
	return "If enabled, this checks each node's `node-role.kubernetes.io/<profile>` label and adds it, set to \"true\", when missing or set to anything else"
}

// Prepare the phase
func (p *LabelNodes) Prepare(ctx context.Context, c *cluster.ZarfCluster, _ *distro.ZarfDistro) error {
	control := p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return h.Configurer.ServiceIsRunning(h, "rke2-server") && h.IsController()
	})
	if len(control) > 0 {
		p.leader = control[0]
	} else {
		logger.From(ctx).Warn("there is no running controllers")
		return ErrNoControllers
	}
	p.ClusterLB = c.Spec.Config.LoadBalancer
	return nil
}

// ShouldRun is true when enabled by flags
func (p *LabelNodes) ShouldRun() bool {
	return p.Enabled
}

// Run the phase
func (p *LabelNodes) Run(ctx context.Context) error {
	l := logger.From(ctx)

	clientset, err := p.clientset()
	if err != nil {
		return fmt.Errorf("failed to build kubernetes client: %w", err)
	}

	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	byName := make(map[string]*corev1.Node, len(nodes.Items))
	for i := range nodes.Items {
		byName[strings.ToLower(nodes.Items[i].Name)] = &nodes.Items[i]
	}

	for _, h := range p.manager.Config.Spec.Hosts {
		if h.Profile == "" {
			continue
		}

		name := strings.ToLower(h.Metadata.Hostname)
		if name == "" {
			name = strings.ToLower(h.Hostname)
		}

		node, ok := byName[name]
		if !ok {
			l.Warn("node not found for host, skipping label check", "host", h, "node", name)
			continue
		}

		labelKey := nodeRoleLabelPrefix + h.Profile
		if node.Labels[labelKey] == "true" {
			continue
		}

		patch, err := json.Marshal(map[string]any{
			"metadata": map[string]any{
				"labels": map[string]string{
					labelKey: "true",
				},
			},
		})
		if err != nil {
			return fmt.Errorf("failed to build label patch for node %s: %w", node.Name, err)
		}

		if _, err := clientset.CoreV1().Nodes().Patch(ctx, node.Name, apitypes.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
			return fmt.Errorf("failed to label node %s with %s=true: %w", node.Name, labelKey, err)
		}

		l.Info("labeled node with profile group", "node", node.Name, "label", labelKey)
	}

	return nil
}

// clientset builds a Kubernetes client from the leader's admin certificates.
func (p *LabelNodes) clientset() (*kubernetes.Clientset, error) {
	if p.leader == nil {
		return nil, errors.New("no leader host resolved")
	}

	ca, err := p.leader.ReadFile("/var/lib/rancher/rke2/server/tls/server-ca.crt")
	if err != nil {
		return nil, fmt.Errorf("failed to read server-ca.crt: %w", err)
	}
	crt, err := p.leader.ReadFile("/var/lib/rancher/rke2/server/tls/client-admin.crt")
	if err != nil {
		return nil, fmt.Errorf("failed to read client-admin.crt: %w", err)
	}
	key, err := p.leader.ReadFile("/var/lib/rancher/rke2/server/tls/client-admin.key")
	if err != nil {
		return nil, fmt.Errorf("failed to read client-admin.key: %w", err)
	}

	restConfig := &rest.Config{
		Host: fmt.Sprintf("https://%s:6443", p.ClusterLB),
		TLSClientConfig: rest.TLSClientConfig{
			CAData:   []byte(ca),
			CertData: []byte(crt),
			KeyData:  []byte(key),
		},
	}

	return kubernetes.NewForConfig(restConfig)
}
