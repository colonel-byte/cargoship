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

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// UpgradeWorkers phase state
type UpgradeWorkers struct {
	UpgradeHosts
	WorkerConcurrent int
}

// Title for the phase
func (p *UpgradeWorkers) Title() string {
	return "Upgrade Worker"
}

// Explanation about the current phase, used for documentation generation
func (p *UpgradeWorkers) Explanation() string {
	return "If the remote node is a worker and is running an older version of the engine, drain the node, stop the service, upgrade the engine, start the service, and uncordon the node by the set concurrency limit"
}

// Prepare the phase
func (p *UpgradeWorkers) Prepare(ctx context.Context, _ *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	control := p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return h.Configurer.ServiceIsRunning(h, p.Distro.GetControllerService()) && h.IsController()
	})
	p.leader = control[0]
	p.hosts = p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return !h.IsController() &&
			h.Metadata.DistroVersion != UnknownVersion &&
			p.VersionLess(h, d.Spec.Version)
	})
	logger.From(ctx).Debug("number of systems that need to be updated", "hosts", len(p.hosts))
	p.service = p.Distro.GetWorkerService()

	return nil
}

// Run the phase
func (p *UpgradeWorkers) Run(ctx context.Context) error {
	return p.batchedParallelWithMessage(
		ctx,
		"upgrading worker nodes",
		p.hosts,
		p.WorkerConcurrent,
		p.drainNode,
		p.stopService,
		p.installDistro,
		p.startService,
		p.waitForNodeReady,
		p.uncordonNode,
	)
}

func (p *UpgradeWorkers) stopService(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("waiting for the service to stop", "service", p.service, "host", h)
	return p.Distro.StopWorkerService(h)
}
