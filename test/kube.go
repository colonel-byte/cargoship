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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// KubeClient builds a Kubernetes clientset from the kubeconfig at $KUBECONFIG.
func (e2e *CargoE2ETest) KubeClient(t *testing.T) (*kubernetes.Clientset, error) {
	t.Helper()
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		return nil, fmt.Errorf("KUBECONFIG is not set")
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build kube config: %w", err)
	}
	return kubernetes.NewForConfig(cfg)
}

// WaitForNodesReady polls the cluster until expectedCount nodes report Ready, or timeout elapses.
func WaitForNodesReady(ctx context.Context, cs *kubernetes.Clientset, expectedCount int, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		nodes, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err == nil {
			ready := 0
			for _, n := range nodes.Items {
				if nodeIsReady(n) {
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

func nodeIsReady(n corev1.Node) bool {
	for _, cond := range n.Status.Conditions {
		if cond.Type == corev1.NodeReady {
			return cond.Status == corev1.ConditionTrue
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
