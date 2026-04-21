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

	"github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/distro"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type UpgradeWorkers struct {
	UpgradeHosts
	WorkerConcurrent int
}

func (p *UpgradeWorkers) Title() string {
	return "Upgrade Worker"
}

// Prepare the phase
func (p *UpgradeWorkers) Prepare(ctx context.Context, c *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	control := p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return h.Configurer.ServiceIsRunning(h, p.Distro.GetControllerService()) && h.IsController()
	})
	p.leader = control[0]
	p.hosts = p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return !h.IsController() &&
			h.Metadata.DistroVersion != UNKNOWN_VERSION &&
			p.VersionLess(h, d.Spec.Version)
	})
	logger.From(ctx).Debug("number of systems that need to be updated", "hosts", len(p.hosts))
	p.service = p.Distro.GetWorkerService()

	return nil
}

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
