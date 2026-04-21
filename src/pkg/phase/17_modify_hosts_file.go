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
	"maps"
	"slices"

	"github.com/colonel-byte/mare/src/api/zarf.dev/v1alpha1/cluster"
	v1alpha1 "github.com/colonel-byte/mare/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/mare/src/api/zarf.dev/v1alpha1/distro"
	"github.com/txn2/txeh"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	host_comment = "added by distro-ctl"
)

// GatherFacts gathers information about hosts, such as if k0s is already up and running
type ModifyHosts struct {
	GenericPhase
	Enabled bool
	hosts   map[string][]string
}

// Title for the phase
func (p *ModifyHosts) Title() string {
	return "Updating hosts file for clusters nodes"
}

// Prepare the phase
func (p *ModifyHosts) Prepare(ctx context.Context, c *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	p.hosts = make(map[string][]string)
	for _, h := range p.manager.Config.Spec.Hosts {
		p.hosts[h.PrivateAddress] = []string{
			h.Configurer.LongHostname(h),
			h.Configurer.Hostname(h),
		}
	}
	logger.From(ctx).Info("nodes that will be updated to etc hosts", "nodes", len(p.hosts))

	return nil
}

// ShouldRun is true when there is a host with selinux or fapolicyd on the hosts
func (p *ModifyHosts) ShouldRun() bool {
	return p.Enabled
}

// Run the phase
func (p *ModifyHosts) Run(ctx context.Context) error {
	return p.parallelDo(ctx, p.manager.Config.Spec.Hosts, p.configureHostsFile)
}

func (p *ModifyHosts) configureHostsFile(ctx context.Context, h *v1alpha1.ZarfHost) error {
	hostsCon, err := h.ReadFile("/etc/hosts")
	if err != nil {
		return err
	}
	hostfile, err := txeh.NewHosts(&txeh.HostsConfig{
		RawText: &hostsCon,
	})

	for _, k := range slices.Sorted(maps.Keys(p.hosts)) {
		hostfile.RemoveAddress(k)
		hostfile.AddHostsWithComment(k, p.hosts[k], host_comment)
	}

	h.WriteFile("/etc/hosts", hostfile.RenderHostsFile(), "0644")

	return nil
}
