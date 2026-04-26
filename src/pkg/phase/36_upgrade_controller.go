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

// UpgradeController phase state
type UpgradeController struct {
	UpgradeHosts
}

// Title for the phase
func (p *UpgradeController) Title() string {
	return "Upgrade Controller"
}

func (p *UpgradeController) Explanation() string {
	return "If the remote node is a controller and is running an older version of the engine, drain the node, stop the service, upgrade the engine, start the service, and uncordon the node sequentially"
}

// Prepare the phase
func (p *UpgradeController) Prepare(ctx context.Context, _ *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	control := p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return h.Configurer.ServiceIsRunning(h, p.Distro.GetControllerService()) && h.IsController()
	})
	p.leader = control[0]
	p.hosts = p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return h.IsController() &&
			h.Metadata.DistroVersion != UnknownVersion &&
			p.VersionLess(h, d.Spec.Version)
	})
	logger.From(ctx).Debug("number of systems that need to be updated", "hosts", len(p.hosts))
	p.service = p.Distro.GetControllerService()

	return nil
}

// Run the phase
func (p *UpgradeController) Run(ctx context.Context) error {
	return p.batchedParallelWithMessage(
		ctx,
		"upgrading controller nodes",
		p.hosts,
		1,
		p.drainNode,
		p.stopService,
		p.installDistro,
		p.startService,
		p.waitForNodeReady,
		p.uncordonNode,
	)
}

func (p *UpgradeController) stopService(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("waiting for the service to stop", "service", p.service, "host", h)
	return p.Distro.StopControllerService(h)
}
