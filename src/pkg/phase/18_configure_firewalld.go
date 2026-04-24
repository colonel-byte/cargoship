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
	"encoding/xml"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/types/distrocfg"
	"github.com/k0sproject/rig/exec"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	// FIREWALLD name of the service for firewalld
	FIREWALLD = "firewalld"
)

// FirewallNodeConfig used to create firewalld config for an array of node ip address
type FirewallNodeConfig struct {
	XMLName xml.Name `xml:"ipset"`
	Type    string   `xml:"type,attr"`
	Short   string   `xml:"short"`
	Long    string   `xml:"description"`
	Entries []string `xml:"entry"`
}

// ConfigureFirewall state
type ConfigureFirewall struct {
	GenericPhase
	Distro    distrocfg.Distro
	Enabled   bool
	hosts     cluster.ZarfHosts
	control   cluster.ZarfHosts
	ipaddress []string
}

// Title for the phase
func (p *ConfigureFirewall) Title() string {
	return "Updating hosts firewalld service"
}

// Prepare the phase
func (p *ConfigureFirewall) Prepare(ctx context.Context, _ *cluster.ZarfCluster, _ *distro.ZarfDistro) error {
	p.hosts = p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return h.Configurer.ServiceIsRunning(h, FIREWALLD)
	})

	p.control = p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return h.Configurer.ServiceIsRunning(h, FIREWALLD) && h.IsController()
	})

	for _, h := range p.hosts {
		p.ipaddress = append(p.ipaddress, h.PrivateAddress)
	}
	logger.From(ctx).Info("nodes with firewalld running", "nodes", len(p.hosts))

	return nil
}

// ShouldRun is true when there is a host with selinux or fapolicyd on the hosts
func (p *ConfigureFirewall) ShouldRun() bool {
	return p.Enabled
}

// Run the phase
func (p *ConfigureFirewall) Run(ctx context.Context) error {
	return p.parallelDo(
		ctx,
		p.hosts,
		p.configureFirewallNodes,
		p.configureFirewallCluster,
		p.updateFirewallCluster,
		p.updateFirewallNode,
		restartFirewall,
	)
}

func (p *ConfigureFirewall) configureFirewallCluster(_ context.Context, h *cluster.ZarfHost) error {
	ent := FirewallNodeConfig{
		Type:    "hash:net",
		Short:   "k8subnets",
		Long:    "IPset for all k8 service and pod CIDRs",
		Entries: p.Distro.GetClusterCIDR(*p.manager.Distro),
	}

	output, err := xml.MarshalIndent(ent, "", "  ")
	if err != nil {
		return err
	}

	return h.WriteFile("/etc/firewalld/ipsets/k8s-subnets.xml", string(output)+"\n", "0600")
}

func (p *ConfigureFirewall) configureFirewallNodes(_ context.Context, h *cluster.ZarfHost) error {
	ent := FirewallNodeConfig{
		Type:    "hash:ip",
		Short:   "k8nodes",
		Long:    "IPset for all k8 nodes",
		Entries: p.ipaddress,
	}

	output, err := xml.MarshalIndent(ent, "", "  ")
	if err != nil {
		return err
	}

	return h.WriteFile("/etc/firewalld/ipsets/k8s-nodes.xml", string(output)+"\n", "0600")
}

func (p *ConfigureFirewall) updateFirewallNode(_ context.Context, h *cluster.ZarfHost) error {
	return h.Execf("firewall-cmd --permanent --zone=trusted --add-source=ipset:k8s-nodes", exec.Sudo(h))
}

func (p *ConfigureFirewall) updateFirewallCluster(_ context.Context, h *cluster.ZarfHost) error {
	return h.Execf("firewall-cmd --permanent --zone=trusted --add-source=ipset:k8s-subnets", exec.Sudo(h))
}

func restartFirewall(_ context.Context, h *cluster.ZarfHost) (err error) {
	return h.Configurer.RestartService(h, FIREWALLD)
}
