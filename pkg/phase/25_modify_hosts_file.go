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

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/txn2/txeh"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	hostComment = "added by cargoship"
)

// hostsFilePath is the file this phase rewrites, named once so the read, the write and the
// change record cannot drift apart.
const hostsFilePath = "/etc/hosts"

// ModifyHosts state
type ModifyHosts struct {
	GenericPhase
	// hostChanges carries Changed and HostsChanged, which this phase can answer honestly
	// because it reads /etc/hosts before rewriting it.
	hostChanges
	Enabled bool
	hosts   map[string][]string
}

// Title for the phase
func (p *ModifyHosts) Title() string {
	return "Updating hosts file for clusters nodes"
}

// Explanation about the current phase, used for documentation generation
func (p *ModifyHosts) Explanation() string {
	return "If enabled, then this will modify the `/etc/hosts` file on the remote nodes with the fully qualified domain name for each node in the cluster"
}

// Prepare the phase
func (p *ModifyHosts) Prepare(ctx context.Context, _ *cluster.Cluster, _ *distro.Distro) error {
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

// ShouldRun is true when this phase is enabled
func (p *ModifyHosts) ShouldRun() bool {
	return p.Enabled
}

// Run the phase
func (p *ModifyHosts) Run(ctx context.Context) error {
	return p.parallelDo(ctx, p.manager.Config.Spec.Hosts, p.configureHostsFile)
}

func (p *ModifyHosts) configureHostsFile(_ context.Context, h *cluster.Host) error {
	hostsCon, err := h.ReadFile(hostsFilePath)
	if err != nil {
		return err
	}
	hostfile, err := txeh.NewHosts(&txeh.HostsConfig{
		RawText: &hostsCon,
	})
	if err != nil {
		return err
	}

	for _, k := range slices.Sorted(maps.Keys(p.hosts)) {
		hostfile.RemoveAddress(k)
		hostfile.AddHostsWithComment(k, p.hosts[k], hostComment)
	}

	// Rendering is deterministic, so a host whose file already names every node renders back
	// byte for byte. Comparing rather than writing regardless is what lets this phase answer
	// whether it changed anything, and it skips a write on the common case of a re-run over a
	// fleet that is already correct.
	rendered := hostfile.RenderHostsFile()
	if rendered == hostsCon {
		p.observed(h)
		return nil
	}

	if err := h.WriteFile(hostsFilePath, rendered, "0644"); err != nil {
		return err
	}
	p.changedFile(h, hostsFilePath, reasonOutOfSync, unifiedDiff(hostsFilePath, hostsCon, rendered))
	return nil
}
