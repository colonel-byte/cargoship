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
	"slices"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/k0sproject/rig/exec"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// DeleteControllers phase state
type DeleteControllers struct {
	DeleteCommon
	NoDrain bool
	hosts   cluster.ZarfHosts
}

// Title for the phase
func (p *DeleteControllers) Title() string {
	return "Reset Controller"
}

// Explanation about the current phase, used for documentation generation
func (p *DeleteControllers) Explanation() string {
	return "Deletes the controller from the cluster, if enabled it will try to drain node before removing the node"
}

// ShouldRun is true when this phase is enabled
func (p *DeleteControllers) ShouldRun() bool {
	return p.leader != nil
}

// Prepare the phase
func (p *DeleteControllers) Prepare(ctx context.Context, c *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	if err := p.DeleteCommon.Prepare(ctx, c, d); err != nil {
		logger.From(ctx).Warn("failed when setting up common logic", "error", err)
	}
	p.hosts = p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		err := p.leader.Exec(p.Distro.KubectlCmdf(*p.leader, p.Distro.DataDirPath(), getNode, h.Configurer.Hostname(h)), exec.Sudo(p.leader))
		if err != nil {
			return false
		}
		return h.IsController() && h.Hostname != p.leader.Hostname
	})
	slices.Reverse(p.hosts)
	logger.From(ctx).Debug("number of systems that need to be reset", "hosts", len(p.hosts))
	return nil
}

// Run the phase
func (p *DeleteControllers) Run(ctx context.Context) error {
	if !p.NoDrain {
		err := p.batchedParallelWithMessage(
			ctx,
			"draining nodes",
			p.hosts,
			1,
			p.drainNode,
		)
		if err != nil {
			logger.From(ctx).Warn("failed to drain node(s), continuing with removing nodes from cluster", "error", err)
		}
	}
	return p.batchedParallelWithMessage(
		ctx,
		"deleting nodes",
		p.hosts,
		1,
		p.deleteNode,
	)
}
