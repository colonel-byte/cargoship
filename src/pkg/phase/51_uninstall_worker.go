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
	"github.com/colonel-byte/cargoship/src/types/distrocfg"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// UninstallWorkers phase state
type UninstallWorkers struct {
	GenericPhase
	Distro           distrocfg.Distro
	WorkerConcurrent int
	hosts            cluster.ZarfHosts
}

// Title for the phase
func (p *UninstallWorkers) Title() string {
	return "Uninstalling Worker"
}

// Explanation about the current phase, used for documentation generation
func (p *UninstallWorkers) Explanation() string {
	return "Remove the rpm, apt, or binary files from a host"
}

// Prepare the phase
func (p *UninstallWorkers) Prepare(ctx context.Context, _ *cluster.ZarfCluster, _ *distro.ZarfDistro) error {
	p.hosts = p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return !h.IsController()
	})
	logger.From(ctx).Debug("number of systems that need to be reset", "hosts", len(p.hosts))

	return nil
}

// Run the phase
func (p *UninstallWorkers) Run(ctx context.Context) error {
	return p.batchedParallelWithMessage(
		ctx,
		"uninstalling engine files",
		p.hosts,
		p.WorkerConcurrent,
		p.uninstallNode,
	)
}

func (p *UninstallWorkers) uninstallNode(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("uninstall", "node", h)
	// rpm: ls /var/lib/rancher/rke2/rpm/* | xargs -I {} rpm -qp {} --queryformat "%{NAME}\n" 2>/dev/null
	return nil
}
