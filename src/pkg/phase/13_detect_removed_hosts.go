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
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/types/distrocfg"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// ErrUnmanagedNodes is returned when the cluster holds a node no host in the config accounts for.
// Apply removes nothing, so continuing would report success over a machine that is still running
// the engine and still joined.
var ErrUnmanagedNodes = errors.New("the cluster holds nodes the config does not: add the host back to the config, run `cargoship install reset` against it, or pass --allow-unmanaged-nodes to apply anyway")

// controlPlaneLabels are the labels a distro puts on a node that runs the control plane. Which
// one is present varies by distro and by version, so any of them counts.
var controlPlaneLabels = []string{
	"node-role.kubernetes.io/control-plane",
	"node-role.kubernetes.io/master",
	"node-role.kubernetes.io/etcd",
}

// DetectRemovedHosts state.
//
// Apply drives the cluster towards the config, but only ever forwards: every phase after this one
// reads Spec.Hosts and acts on what it finds there. A host deleted from the config is therefore
// invisible to all of them, and the machine keeps running the engine and stays joined to the
// cluster while apply reports success. This phase is what stops that from being silent.
//
// It detects and refuses. It does not reconcile, and the reason is not effort: apply cannot reach
// the machine it would have to clean up. Draining a node and deleting its Node object needs only
// the name, which the API gives us. Uninstalling the engine needs an SSH connection, and the
// address, user and key for that lived in the host block that was just deleted. Deleting the Node
// object without the uninstall is worse than doing nothing, because it looks like the removal
// worked while the machine keeps its certificates and tries to rejoin -- and for a controller it
// stays an etcd member, so the quorum arithmetic silently moves against a member nobody can reach.
//
// The layer that does hold the connection details for a removed host is the OpenTofu provider,
// which still has the old host block in its state file when it plans the destroy. That is where a
// real removal path belongs, and it can call reset against exactly that host. Until then this
// phase makes the gap loud instead of invisible.
type DetectRemovedHosts struct {
	GenericPhase
	Distro    distrocfg.Distro
	ClusterLB string
	// AllowUnmanaged downgrades the refusal to a warning. A cluster can hold nodes cargoship
	// never joined, and refusing on those would block every apply on a working setup, so the
	// operator needs a way to say the extra nodes are deliberate.
	AllowUnmanaged bool
	leader         *cluster.ZarfHost
}

// Title for the phase
func (p *DetectRemovedHosts) Title() string {
	return "Checking for nodes no longer in the config"
}

// Explanation about the current phase, used for documentation generation
func (p *DetectRemovedHosts) Explanation() string {
	return "Compares the nodes joined to the cluster against the hosts in the config and stops the apply when the cluster holds a node the config does not, since nothing later in an apply removes a node"
}

// ReadOnly marks this phase safe under a dry run, and returns the reason for the phase docs.
func (p *DetectRemovedHosts) ReadOnly() string {
	return "Checking for removed nodes lists the nodes joined to the cluster and compares them to the config. It writes nothing, and a dry run is exactly when an operator wants to be told that a host they deleted from the config is still running."
}

// Prepare the phase.
//
// The leader is any host in the config already running the controller service, which is the same
// way the label and delete phases find one. Nothing is running on a first install, so there is no
// leader, and ShouldRun turns the phase off: a cluster that does not exist yet cannot have had a
// host removed from it.
func (p *DetectRemovedHosts) Prepare(_ context.Context, c *cluster.ZarfCluster, _ *distro.ZarfDistro) error {
	control := p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return h.IsController() && h.Configurer.ServiceIsRunning(h, p.Distro.GetControllerService())
	})
	if len(control) > 0 {
		p.leader = control[0]
	}
	p.ClusterLB = c.Spec.Config.LoadBalancer
	return nil
}

// ShouldRun is true once there is a running controller to ask
func (p *DetectRemovedHosts) ShouldRun() bool {
	return p.leader != nil
}

// Run the phase.
//
// Only positive evidence stops the apply. Failing to reach the API server says nothing about
// whether a host was removed, and this phase runs before every phase that does the real work, so
// treating an unreachable cluster as a refusal would turn a load balancer blip into a failed apply
// that had nothing to do with removals. Those cases warn and continue.
func (p *DetectRemovedHosts) Run(ctx context.Context) error {
	clientset, err := p.clientset()
	if err != nil {
		logger.From(ctx).Warn("cannot check for removed hosts, skipping the check", "error", err)
		return nil
	}

	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		logger.From(ctx).Warn("cannot list nodes, skipping the check for removed hosts", "error", err)
		return nil
	}

	unmanaged := unmanagedNodes(p.manager.Config.Spec.Hosts, nodes.Items)
	if len(unmanaged) == 0 {
		return nil
	}

	if p.AllowUnmanaged {
		logger.From(ctx).Warn(
			"the cluster holds nodes the config does not, and this apply will leave them alone",
			"nodes", strings.Join(unmanaged, ", "),
		)
		return nil
	}

	return fmt.Errorf("%w: %s", ErrUnmanagedNodes, strings.Join(unmanaged, ", "))
}

// unmanagedNodes returns the names of the nodes joined to the cluster that no host in the config
// accounts for, sorted, each tagged with its role so the message says how dangerous it is to leave
// one behind.
//
// Matching is deliberately generous, because a false positive here stops an apply on a working
// cluster. A host contributes every identifier cargoship knows for it: the hostname the facts
// phase read off the machine, the hostname in the config for a host not yet visited, the private
// address, and the connection address. A node contributes its name and every address it reports.
// Any overlap means the node is managed.
//
// The name alone is not enough. A node's name is usually its hostname, but an operator can set
// `node-name` in the engine config and break that, and matching on names alone would then report
// every node in the cluster as removed. The addresses are what survive that.
//
// The comparison is case-insensitive: a node name is always lowercase, while a config hostname
// need not be.
func unmanagedNodes(hosts cluster.ZarfHosts, nodes []corev1.Node) []string {
	known := make(map[string]struct{}, len(hosts)*4)
	for _, h := range hosts {
		for _, id := range []string{h.Metadata.Hostname, h.Hostname, h.PrivateAddress, h.Address()} {
			if id != "" {
				known[strings.ToLower(id)] = struct{}{}
			}
		}
	}

	var unmanaged []string
	for i := range nodes {
		if isKnownNode(&nodes[i], known) {
			continue
		}
		unmanaged = append(unmanaged, fmt.Sprintf("%s (%s)", nodes[i].Name, nodeRole(&nodes[i])))
	}
	sort.Strings(unmanaged)
	return unmanaged
}

// isKnownNode reports whether any identifier the node carries names a host in the config.
func isKnownNode(node *corev1.Node, known map[string]struct{}) bool {
	if _, ok := known[strings.ToLower(node.Name)]; ok {
		return true
	}
	for _, addr := range node.Status.Addresses {
		if addr.Address == "" {
			continue
		}
		if _, ok := known[strings.ToLower(addr.Address)]; ok {
			return true
		}
	}
	return false
}

// nodeRole is "controller" for a node carrying any of the control plane labels, "worker"
// otherwise. Removing a controller is the dangerous case: its etcd membership outlives the Node
// object, so a cluster that looks smaller is still counting it towards quorum.
func nodeRole(node *corev1.Node) string {
	for _, label := range controlPlaneLabels {
		if _, ok := node.Labels[label]; ok {
			return "controller"
		}
	}
	return "worker"
}

// clientset builds a Kubernetes client from the leader's admin certificates.
func (p *DetectRemovedHosts) clientset() (*kubernetes.Clientset, error) {
	if p.leader == nil {
		return nil, ErrNoControllers
	}
	return distroClientset(p.Distro, *p.leader, p.ClusterLB)
}
