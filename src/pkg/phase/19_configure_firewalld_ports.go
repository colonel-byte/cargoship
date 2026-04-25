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
	"time"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/pkg/retry"
	"github.com/k0sproject/rig/exec"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// FirewallPortConfig used to create firewalld config for an array of port(s)
type FirewallPortConfig struct {
	XMLName xml.Name               `xml:"service"`
	Short   string                 `xml:"short"`
	Ports   []cluster.ZarfHostPort `xml:"port"`
}

// ConfigureFirewallPorts gathers information about hosts, such as if the engine is already up and running
type ConfigureFirewallPorts struct {
	GenericPhase
	Enabled bool
	hosts   cluster.ZarfHosts
}

// Title for the phase
func (p *ConfigureFirewallPorts) Title() string {
	return "Updating hosts firewalld ports"
}

// Prepare the phase
func (p *ConfigureFirewallPorts) Prepare(ctx context.Context, _ *cluster.ZarfCluster, _ *distro.ZarfDistro) error {
	p.hosts = p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return h.Configurer.ServiceIsRunning(h, FIREWALLD) && len(h.Ports) > 0
	})

	logger.From(ctx).Info("nodes that need ports exposed", "nodes", len(p.hosts))

	return nil
}

// ShouldRun is true when there is a host with selinux or fapolicyd on the hosts
func (p *ConfigureFirewallPorts) ShouldRun() bool {
	return p.Enabled
}

// Run the phase
func (p *ConfigureFirewallPorts) Run(ctx context.Context) error {
	return retry.Timeout(ctx, 30*time.Second, func(ctx context.Context) error {
		return p.parallelDo(
			ctx,
			p.hosts,
			p.configureFirewallPorts,
			p.enableFirewallExposedPorts,
			restartFirewall,
		)
	})
}

func (p *ConfigureFirewallPorts) configureFirewallPorts(_ context.Context, h *cluster.ZarfHost) error {
	ent := FirewallPortConfig{
		Short: "distro-exposed-ports",
		Ports: h.Ports,
	}

	output, err := xml.MarshalIndent(ent, "", "  ")
	if err != nil {
		return err
	}

	return h.WriteFile("/etc/firewalld/services/distro-exposed-ports.xml", string(output)+"\n", "0600")
}

func (p *ConfigureFirewallPorts) enableFirewallExposedPorts(_ context.Context, h *cluster.ZarfHost) error {
	return h.Execf("firewall-cmd --permanent --zone=public --add-service=distro-exposed-ports", exec.Sudo(h))
}
