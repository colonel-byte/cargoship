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

	"github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/cluster"
	v1alpha1 "github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/distro"
	"github.com/k0sproject/rig/exec"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	FIREWALLD = "firewalld"
)

type FirewallConfig struct {
	XMLName xml.Name `xml:"ipset"`
	Type    string   `xml:"type,attr"`
	Short   string   `xml:"short"`
	Long    string   `xml:"description"`
	Entries []string `xml:"entry"`
}

// GatherFacts gathers information about hosts, such as if k0s is already up and running
type ConfigureFirewall struct {
	GenericPhase
	Enabled   bool
	hosts     cluster.ZarfHosts
	ipaddress []string
}

// Title for the phase
func (p *ConfigureFirewall) Title() string {
	return "Updating hosts file for clusters nodes"
}

// Prepare the phase
func (p *ConfigureFirewall) Prepare(ctx context.Context, c *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	p.hosts = p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return h.Configurer.ServiceIsRunning(h, FIREWALLD)
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
		p.updateFirewalld,
		p.restartFirewall,
	)
}

func (p *ConfigureFirewall) restartFirewall(ctx context.Context, h *v1alpha1.ZarfHost) (err error) {
	return h.Configurer.RestartService(h, FIREWALLD)
}

func (p *ConfigureFirewall) configureFirewallCluster(ctx context.Context, h *v1alpha1.ZarfHost) error {
	ent := FirewallConfig{
		Type:  "hash:net",
		Short: "k8subnets",
		Long:  "IPset for all k8 service and pod CIDRs",
		Entries: []string{
			"10.42.0.0/16",
			"10.43.0.0/16",
		},
	}

	output, err := xml.MarshalIndent(ent, "", "  ")
	if err != nil {
		return err
	}

	return h.WriteFile("/etc/firewalld/ipsets/k8s-subnets.xml", string(output)+"\n", "0600")
}

func (p *ConfigureFirewall) configureFirewallNodes(ctx context.Context, h *v1alpha1.ZarfHost) error {
	ent := FirewallConfig{
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

func (p *ConfigureFirewall) updateFirewalld(ctx context.Context, h *v1alpha1.ZarfHost) error {
	if err := h.Execf("firewall-cmd --permanent --zone=trusted --add-source=ipset:k8s-nodes", exec.Sudo(h)); err != nil {
		return err
	}
	if err := h.Execf("firewall-cmd --permanent --zone=trusted --add-source=ipset:k8s-subnets", exec.Sudo(h)); err != nil {
		return err
	}
	return nil
}
