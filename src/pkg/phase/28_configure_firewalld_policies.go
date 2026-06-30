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
	"fmt"
	"regexp"
	"time"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/pkg/retry"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// ConfigureFirewallPolicy updates the host system firewalld policy files.
type ConfigureFirewallPolicy struct {
	GenericPhase
	Enabled bool
	hosts   cluster.ZarfHosts
}

// Title for the phase
func (p *ConfigureFirewallPolicy) Title() string {
	return "Updating hosts firewalld policies"
}

// Explanation about the current phase, used for documentation generation
func (p *ConfigureFirewallPolicy) Explanation() string {
	return "If enabled, this create firewalld policies. This is controlled by the inventory file."
}

// Prepare the phase
func (p *ConfigureFirewallPolicy) Prepare(ctx context.Context, _ *cluster.ZarfCluster, _ *distro.ZarfDistro) error {
	p.hosts = p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return h.Configurer.ServiceIsRunning(h, FIREWALLD) && len(h.Policy) > 0
	})

	logger.From(ctx).Info("nodes that need ports exposed", "nodes", len(p.hosts))

	return nil
}

// ShouldRun is true when there is a host with selinux or fapolicyd on the hosts
func (p *ConfigureFirewallPolicy) ShouldRun() bool {
	return p.Enabled
}

// Run the phase
func (p *ConfigureFirewallPolicy) Run(ctx context.Context) error {
	return retry.Timeout(ctx, 30*time.Second, func(ctx context.Context) error {
		return p.parallelDo(
			ctx,
			p.hosts,
			p.configureFirewallPolicy,
			restartFirewall,
		)
	})
}

func (p *ConfigureFirewallPolicy) configureFirewallPolicy(_ context.Context, h *cluster.ZarfHost) error {
	if len(h.Policy) > 0 {
		for key, value := range h.Policy {
			value.Short = "Cargoship Policy"
			output, err := xml.MarshalIndent(value, "", "  ")
			if err != nil {
				return err
			}
			out := regexp.MustCompile(`></.+>`).ReplaceAllString(string(output), "/>")
			err = h.WriteFile(fmt.Sprintf("/etc/firewalld/policies/%s.xml", key), out+"\n", "0600")
			if err != nil {
				return err
			}
		}
	}
	return nil
}
