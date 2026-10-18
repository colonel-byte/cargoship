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

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

// nodeRoleLabelPrefix prefixes the Kubernetes node label cargoship uses to mark which
// profile group a node belongs to.
const nodeRoleLabelPrefix = "node-role.kubernetes.io/"

// nodesResource names the resource this phase reads and patches.
//
// The node list goes through the dynamic client rather than the typed clientset,
// because every typed client imports k8s.io/client-go/kubernetes/scheme, which
// registers every Kubernetes API group and linked about 24MB of k8s.io/api and
// client-go into the binary. This phase reads two fields off a node and patches one
// label, which unstructured serves directly through GetName and GetLabels.
var nodesResource = schema.GroupVersionResource{Version: "v1", Resource: "nodes"}

// LabelNodes phase state
type LabelNodes struct {
	GenericPhase
	Distro    distrocfg.Distro
	ClusterLB string
	Enabled   bool
	leader    *cluster.Host
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
func (p *LabelNodes) Prepare(ctx context.Context, c *cluster.Cluster, _ *distro.Distro) error {
	control := p.manager.Config.Spec.Hosts.Filter(func(h *cluster.Host) bool {
		return h.ServiceIsRunning(ctx, p.Distro.GetControllerService()) && h.IsController()
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

	client, err := p.dynamicClient()
	if err != nil {
		return fmt.Errorf("failed to build kubernetes client: %w", err)
	}
	nodeClient := client.Resource(nodesResource)

	nodes, err := nodeClient.List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	byName := make(map[string]*unstructured.Unstructured, len(nodes.Items))
	for i := range nodes.Items {
		byName[strings.ToLower(nodes.Items[i].GetName())] = &nodes.Items[i]
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
		if node.GetLabels()[labelKey] == "true" {
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
			return fmt.Errorf("failed to build label patch for node %s: %w", node.GetName(), err)
		}

		if _, err := nodeClient.Patch(ctx, node.GetName(), apitypes.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
			return fmt.Errorf("failed to label node %s with %s=true: %w", node.GetName(), labelKey, err)
		}

		l.Info("labeled node with profile group", "node", node.GetName(), "label", labelKey)
	}

	return nil
}

// dynamicClient builds a Kubernetes client from the leader's admin certificates.
func (p *LabelNodes) dynamicClient() (dynamic.Interface, error) {
	if p.leader == nil {
		return nil, errors.New("no leader host resolved")
	}

	creds, err := p.Distro.AdminCredentials(p.leader, p.Distro.DataDirPath())
	if err != nil {
		return nil, fmt.Errorf("failed to read admin credentials: %w", err)
	}

	restConfig := &rest.Config{
		Host: fmt.Sprintf("https://%s:6443", p.ClusterLB),
		TLSClientConfig: rest.TLSClientConfig{
			CAData:   creds.CertificateAuthority,
			CertData: creds.ClientCertificate,
			KeyData:  creds.ClientKey,
		},
	}

	return dynamic.NewForConfig(restConfig)
}
