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
	"fmt"
	"time"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/pkg/firewall"
	"github.com/colonel-byte/cargoship/src/pkg/retry"
	"github.com/colonel-byte/cargoship/src/types/distrocfg"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	// FIREWALLD name of the service for firewalld
	FIREWALLD = firewall.FirewalldService
	// UFW name of the service for ufw
	UFW = firewall.UFWService
)

// ConfigureFirewall state
type ConfigureFirewall struct {
	GenericPhase
	Distro    distrocfg.Distro
	Enabled   bool
	hosts     cluster.ZarfHosts
	backends  map[string]firewall.Backend
	ipaddress []string
}

// Title for the phase
func (p *ConfigureFirewall) Title() string {
	return "Updating hosts firewall"
}

// Explanation about the current phase, used for documentation generation
func (p *ConfigureFirewall) Explanation() string {
	return "If enabled, this configures the firewall on each node that runs one, firewalld or ufw. " +
		"It trusts every other node in the cluster along with the engine's pod and service CIDRs, " +
		"opens the ports in the `.host.ports` section, and applies the rules in the `.host.firewall.rules` section"
}

// Prepare the phase
func (p *ConfigureFirewall) Prepare(ctx context.Context, _ *cluster.ZarfCluster, _ *distro.ZarfDistro) error {
	p.backends = make(map[string]firewall.Backend)

	p.hosts = p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		backend := firewall.For(h)
		if backend == nil {
			return false
		}
		p.backends[h.String()] = backend

		return true
	})

	for _, h := range p.manager.Config.Spec.Hosts {
		if h.PrivateAddress != "" {
			p.ipaddress = append(p.ipaddress, h.PrivateAddress)
		}
	}

	for _, backend := range firewall.Backends() {
		count := len(p.hosts.Filter(func(h *cluster.ZarfHost) bool {
			return p.backends[h.String()].Name() == backend.Name()
		}))
		logger.From(ctx).Info("nodes with a firewall running", "firewall", backend.Name(), "nodes", count)
	}

	return nil
}

// ShouldRun is true when the firewall is being managed and at least one node runs one
func (p *ConfigureFirewall) ShouldRun() bool {
	return p.Enabled && len(p.hosts) > 0
}

// Run the phase
func (p *ConfigureFirewall) Run(ctx context.Context) error {
	return retry.Timeout(ctx, 30*time.Second, func(ctx context.Context) error {
		return p.parallelDo(ctx, p.hosts, p.configureFirewall)
	})
}

func (p *ConfigureFirewall) configureFirewall(ctx context.Context, h *cluster.ZarfHost) error {
	backend := p.backends[h.String()]
	if backend == nil {
		return nil
	}

	plan := firewall.Plan{
		NodeAddresses: p.ipaddress,
		ClusterCIDRs:  p.Distro.GetClusterCIDR(*p.manager.Distro),
		Ports:         h.Host.Ports,
		Rules:         h.Host.Firewall.Rules,
		Policies:      h.Host.Policy,
	}

	if plan.IsEmpty() {
		return nil
	}

	if err := backend.Apply(ctx, h, plan); err != nil {
		return fmt.Errorf("%s: failed to configure %s: %w", h, backend.Name(), err)
	}

	return nil
}
