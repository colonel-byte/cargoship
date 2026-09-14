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
	"testing"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// joinedNode is a node joined to the cluster with the labels its role implies.
func joinedNode(name string, controller bool) corev1.Node {
	labels := map[string]string{}
	if controller {
		labels["node-role.kubernetes.io/control-plane"] = "true"
		labels["node-role.kubernetes.io/etcd"] = "true"
	}
	return corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
}

// configHost is a host still in the config, with the hostname the facts phase read off it.
func configHost(name string) *cluster.ZarfHost {
	h := &cluster.ZarfHost{Hostname: name}
	h.Metadata.Hostname = name
	return h
}

// TestUnmanagedNodesFindsARemovedWorker is one half of what #306 asks for: an apply run after a
// worker's host block was deleted from the config must see the node that is still joined.
func TestUnmanagedNodesFindsARemovedWorker(t *testing.T) {
	hosts := cluster.ZarfHosts{configHost("control1"), configHost("worker1")}
	nodes := []corev1.Node{joinedNode("control1", true), joinedNode("worker1", false), joinedNode("worker2", false)}

	require.Equal(t, []string{"worker2 (worker)"}, unmanagedNodes(hosts, nodes))
}

// TestUnmanagedNodesFindsARemovedController is the other half, and the dangerous one. The role in
// the message is what tells the operator that the leftover node is still an etcd member, so the
// quorum arithmetic already moved against them.
func TestUnmanagedNodesFindsARemovedController(t *testing.T) {
	hosts := cluster.ZarfHosts{configHost("control1"), configHost("worker1")}
	nodes := []corev1.Node{joinedNode("control1", true), joinedNode("control2", true), joinedNode("worker1", false)}

	require.Equal(t, []string{"control2 (controller)"}, unmanagedNodes(hosts, nodes))
}

// TestUnmanagedNodesUsesTheMasterLabel covers a distro that still labels its control plane the old
// way. Missing that label would report a controller as a worker and understate the risk.
func TestUnmanagedNodesUsesTheMasterLabel(t *testing.T) {
	old := corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   "control2",
		Labels: map[string]string{"node-role.kubernetes.io/master": "true"},
	}}

	require.Equal(t, []string{"control2 (controller)"}, unmanagedNodes(nil, []corev1.Node{old}))
}

// TestUnmanagedNodesIsEmptyWhenTheConfigMatches pins the case that must stay silent, since this
// phase runs on every apply against an existing cluster.
func TestUnmanagedNodesIsEmptyWhenTheConfigMatches(t *testing.T) {
	hosts := cluster.ZarfHosts{configHost("control1"), configHost("worker1")}
	nodes := []corev1.Node{joinedNode("control1", true), joinedNode("worker1", false)}

	require.Empty(t, unmanagedNodes(hosts, nodes))
}

// TestUnmanagedNodesMatchesCaseInsensitively guards a false positive that would stop every apply
// on a cluster whose config spells a hostname with capitals. Node names are always lowercase.
func TestUnmanagedNodesMatchesCaseInsensitively(t *testing.T) {
	hosts := cluster.ZarfHosts{configHost("Control1.EXAMPLE.com")}

	require.Empty(t, unmanagedNodes(hosts, []corev1.Node{joinedNode("control1.example.com", true)}))
}

// TestUnmanagedNodesFallsBackToTheConfigHostname covers a host the facts phase has not visited, so
// Metadata.Hostname is empty. Without the fallback every such host would look removed.
func TestUnmanagedNodesFallsBackToTheConfigHostname(t *testing.T) {
	hosts := cluster.ZarfHosts{{Hostname: "worker1"}}

	require.Empty(t, unmanagedNodes(hosts, []corev1.Node{joinedNode("worker1", false)}))
}

// TestUnmanagedNodesSortsAndReportsAll keeps the message stable, so an operator comparing two runs
// sees the same order, and pins that every leftover node is named rather than just the first.
func TestUnmanagedNodesSortsAndReportsAll(t *testing.T) {
	nodes := []corev1.Node{joinedNode("worker9", false), joinedNode("control2", true), joinedNode("worker3", false)}

	require.Equal(t, []string{
		"control2 (controller)",
		"worker3 (worker)",
		"worker9 (worker)",
	}, unmanagedNodes(nil, nodes))
}

// TestUnmanagedNodesMatchesOnAddressWhenTheNameWasOverridden is the false-positive that matters
// most. An operator who sets `node-name` in the engine config gets node names that match no
// hostname anywhere, and matching on names alone would report every node in the cluster as removed
// and stop every apply. The address the node reports is what still ties it to its host.
func TestUnmanagedNodesMatchesOnAddressWhenTheNameWasOverridden(t *testing.T) {
	h := &cluster.ZarfHost{Hostname: "worker1", PrivateAddress: "10.0.0.7"}
	h.Metadata.Hostname = "worker1"

	renamed := joinedNode("custom-node-name", false)
	renamed.Status.Addresses = []corev1.NodeAddress{
		{Type: corev1.NodeInternalIP, Address: "10.0.0.7"},
	}

	require.Empty(t, unmanagedNodes(cluster.ZarfHosts{h}, []corev1.Node{renamed}))
}

// TestUnmanagedNodesStillCatchesARemovalWithAddresses guards the other direction: the address
// matching must not be so loose that a genuinely removed node looks managed.
func TestUnmanagedNodesStillCatchesARemovalWithAddresses(t *testing.T) {
	h := &cluster.ZarfHost{Hostname: "worker1", PrivateAddress: "10.0.0.7"}
	h.Metadata.Hostname = "worker1"

	gone := joinedNode("worker2", false)
	gone.Status.Addresses = []corev1.NodeAddress{
		{Type: corev1.NodeInternalIP, Address: "10.0.0.8"},
		{Type: corev1.NodeHostName, Address: "worker2"},
	}

	require.Equal(t, []string{"worker2 (worker)"}, unmanagedNodes(cluster.ZarfHosts{h}, []corev1.Node{gone}))
}

// TestUnmanagedNodesIgnoresEmptyIdentifiers pins that a host with no private address yet, and a
// node reporting a blank address, do not match each other through the empty string.
func TestUnmanagedNodesIgnoresEmptyIdentifiers(t *testing.T) {
	blank := joinedNode("worker2", false)
	blank.Status.Addresses = []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: ""}}

	require.Equal(t, []string{"worker2 (worker)"},
		unmanagedNodes(cluster.ZarfHosts{{Hostname: "worker1"}}, []corev1.Node{blank}))
}

// TestDetectRemovedHostsSkipsWithoutARunningController is the first-install case. Nothing is
// running, so there is no leader to ask, and a cluster that does not exist cannot have lost a
// host. Prepare must not fail on the way to that, since it runs before ShouldRun is consulted.
func TestDetectRemovedHostsSkipsWithoutARunningController(t *testing.T) {
	p := &DetectRemovedHosts{}
	m := &Manager{Config: &cluster.ZarfCluster{Spec: cluster.ZarfClusterSpec{
		Hosts: cluster.ZarfHosts{{Hostname: "node1", Configurer: &stoppedConfigurer{}}},
	}}}
	p.SetManager(m)

	require.NoError(t, p.Prepare(context.Background(), m.Config, nil))
	require.False(t, p.ShouldRun())
}

// TestDetectRemovedHostsIsReadOnly pins the phase into the dry run. A dry run is the run where
// being told about a host you deleted from the config costs nothing, so it is the one that most
// needs this check.
func TestDetectRemovedHostsIsReadOnly(t *testing.T) {
	require.Equal(t, DryRunReadOnly, ClassifyDryRun(&DetectRemovedHosts{}))
	require.NotEmpty(t, DryRunNote(&DetectRemovedHosts{}))
}
