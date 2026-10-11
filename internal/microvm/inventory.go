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

package microvm

import (
	"fmt"
	"os"

	apicluster "github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	goyaml "github.com/goccy/go-yaml"
	rig "github.com/k0sproject/rig/v2"
	"github.com/k0sproject/rig/v2/protocol/ssh"
)

// sshUser is the account every node trusts the fleet key for. Root, because the phases this
// fleet exists to exercise are privileged and cargoship reaches them through Sudo() either
// way; a wrapper account would add a layer that is not under test.
const sshUser = "root"

// lanInterface is the guest's name for the interface on the fleet's layer 2 segment.
//
// It is pinned here rather than discovered because it is written into the inventory as
// PrivateInterface, and the inventory has to be correct before any node has been asked
// anything. On q35 with this command line the two virtio NICs land at enp0s3 and enp0s4 in
// the order they appear, so the second is the private one. Adding a PCI device ahead of them
// would move both, which is the reason the MAC addresses -- not these names -- are what
// cloud-init matches on.
const lanInterface = "enp0s4"

// Inventory renders a ZarfCluster inventory for a fleet.
//
// PrivateAddress and PrivateInterface are both set explicitly, which the bootloose equivalent
// does not do. They have to be: a node's default route is on the slirp management interface,
// so fact gathering would discover 10.0.2.15 -- the same address on every node, and reachable
// from none of them. Every phase that distributes peer addresses, which is the firewall and
// the hosts file, would then distribute addresses that do not work.
func Inventory(f Fleet) apicluster.ZarfCluster {
	hosts := make(apicluster.ZarfHosts, 0, len(f.Nodes))
	for _, n := range f.Nodes {
		hosts = append(hosts, hostFromNode(n, f.KeyPath))
	}

	return apicluster.ZarfCluster{
		Kind: "ZarfCluster",
		Metadata: apicluster.ZarfClusterMetadata{
			Name: "microvm-" + f.Name,
		},
		Spec: apicluster.ZarfClusterSpec{
			Config: apicluster.ZarfClusterConfig{
				// The leader's address on the private segment, not its forwarded loopback
				// port: this is the address the other nodes join through, and they reach each
				// other over that segment and not through the host.
				LoadBalancer: f.Nodes[0].PrivateAddress,
			},
			Hosts: hosts,
		},
	}
}

// hostFromNode maps one node onto an inventory host.
func hostFromNode(n Node, keyPath string) *apicluster.ZarfHost {
	return &apicluster.ZarfHost{
		Hostname:         n.Name,
		PrivateAddress:   n.PrivateAddress,
		PrivateInterface: lanInterface,
		Role:             n.Role,
		// The profile doubles as the node-role.kubernetes.io/<profile> label the LabelNodes
		// phase writes, so a profile matching the role is what makes that phase observable.
		// It names no entry in the cluster config's profile map, which leaves per-profile
		// concurrency on its default.
		Profile: n.Role,
		ClientWithConfig: rig.ClientWithConfig{
			ConnectionConfig: rig.CompositeConfig{
				SSH: &ssh.Config{
					Address: "127.0.0.1",
					User:    sshUser,
					Port:    n.SSHPort,
					KeyPath: &keyPath,
				},
			},
		},
	}
}

// writeInventory marshals the fleet's inventory to its directory, with the same library
// cargoship parses a ZarfCluster with.
func writeInventory(f Fleet) error {
	b, err := goyaml.Marshal(Inventory(f))
	if err != nil {
		return fmt.Errorf("marshalling the inventory for fleet %s: %w", f.Name, err)
	}
	if err := os.WriteFile(f.InventoryPath(), b, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", f.InventoryPath(), err)
	}
	return nil
}
