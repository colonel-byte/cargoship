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

package test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

// NodesResource names the node resource for the dynamic client. The suite reads node
// names, labels and two spec/status fields, and the typed clientset would link every
// Kubernetes API group into the test binary for it.
var NodesResource = schema.GroupVersionResource{Version: "v1", Resource: "nodes"}

// KubeClient builds a Kubernetes client from the kubeconfig at $KUBECONFIG.
func (e2e *CargoE2ETest) KubeClient(t *testing.T) (dynamic.Interface, error) {
	t.Helper()
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		return nil, fmt.Errorf("KUBECONFIG is not set")
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build kube config: %w", err)
	}
	return dynamic.NewForConfig(cfg)
}

// WaitForNodesReady polls the cluster until expectedCount nodes report Ready, or timeout elapses.
func WaitForNodesReady(ctx context.Context, cs dynamic.Interface, expectedCount int, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		nodes, err := cs.Resource(NodesResource).List(ctx, metav1.ListOptions{})
		if err == nil {
			ready := 0
			for i := range nodes.Items {
				if nodeIsReady(&nodes.Items[i]) {
					ready++
				}
			}
			if ready >= expectedCount {
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for %d ready nodes: %w", expectedCount, ctx.Err())
		case <-ticker.C:
		}
	}
}

// nodeIsReady reads the Ready condition out of an unstructured node. A missing or
// malformed status reads as not ready, which is the right answer while a node is
// still coming up.
func nodeIsReady(n *unstructured.Unstructured) bool {
	conditions, found, err := unstructured.NestedSlice(n.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}
	for _, c := range conditions {
		cond, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if cond["type"] == "Ready" {
			return cond["status"] == "True"
		}
	}
	return false
}

// CleanFiles removes paths, failing the test on error rather than relying on .gitignore alone.
func CleanFiles(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.RemoveAll(p); err != nil {
			t.Errorf("failed to clean up %s: %v", p, err)
		}
	}
}
