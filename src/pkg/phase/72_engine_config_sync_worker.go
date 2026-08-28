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
	"sync"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// EngineConfigSyncWorker phase state
type EngineConfigSyncWorker struct {
	EngineConfigSyncHosts
	WorkerConcurrent int
}

// Title for the phase
func (p *EngineConfigSyncWorker) Title() string {
	return "Sync Registry Config Worker"
}

// Explanation about the current phase, used for documentation generation
func (p *EngineConfigSyncWorker) Explanation() string {
	return "If the remote node is a worker and its engine config (registries/audit/pss) has drifted from the desired state, drain the node, stop the service, write the new config, start the service, and uncordon the node by the set concurrency limit"
}

// Prepare the phase
func (p *EngineConfigSyncWorker) Prepare(ctx context.Context, c *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	if err := p.loadDesiredConfig(c, *d); err != nil {
		return err
	}
	if err := p.prepareLeader(); err != nil {
		return err
	}

	candidates := p.manager.Config.Spec.Hosts.Workers()
	var mu sync.Mutex
	var matched cluster.ZarfHosts
	if err := p.parallelDo(ctx, candidates, func(_ context.Context, h *cluster.ZarfHost) error {
		if p.needsUpdate(h) {
			mu.Lock()
			matched = append(matched, h)
			mu.Unlock()
		}
		return nil
	}); err != nil {
		return err
	}
	p.hosts = matched
	p.service = p.Distro.GetWorkerService()

	logger.From(ctx).Debug("number of workers that need config synced", "hosts", len(p.hosts))

	return nil
}

// Run the phase
func (p *EngineConfigSyncWorker) Run(ctx context.Context) error {
	return p.batchedParallelWithMessage(
		ctx,
		"syncing worker config",
		p.hosts,
		p.WorkerConcurrent,
		p.drainNode,
		p.stopService,
		p.writeFiles,
		p.startService,
		p.waitForNodeReady,
		p.uncordonNode,
	)
}

func (p *EngineConfigSyncWorker) stopService(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("waiting for the service to stop", "service", p.service, "host", h)
	return p.Distro.StopWorkerService(h)
}
