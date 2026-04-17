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
	"github.com/colonel-byte/zarf-distro/src/types/distrocfg"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type InitializeWorkers struct {
	GenericPhase
	Distro           distrocfg.Distro
	worker           cluster.ZarfHosts
	WorkerConcurrent int
}

func (p *InitializeWorkers) Title() string {
	return "Initialize Worker"
}

// Prepare the phase
func (p *InitializeWorkers) Prepare(ctx context.Context, c *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	p.worker = p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return !h.Configurer.ServiceIsRunning(h, p.Distro.GetWorkerService()) && !h.IsController()
	})
	logger.From(ctx).Info("number of systems that need to be started", "hosts", len(p.worker))

	return nil
}

// ShouldRun is true when there are workers
func (p *InitializeWorkers) ShouldRun() bool {
	return len(p.worker) > 0
}

func (p *InitializeWorkers) Run(ctx context.Context) error {
	if p.WorkerConcurrent == 0 {
		return p.worker.ParallelEach(
			ctx,
			p.startService,
			p.enableService,
		)
	}
	return p.worker.BatchedParallelEach(
		ctx,
		p.WorkerConcurrent,
		p.startService,
		p.enableService,
	)
}

func (p *InitializeWorkers) startService(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("waiting for the worker service to start", "service", p.Distro.GetWorkerService(), "host", h)
	return h.Configurer.StartService(h, p.Distro.GetWorkerService())
}

func (p *InitializeWorkers) enableService(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("enabling the worker service", "service", p.Distro.GetWorkerService(), "host", h)
	return h.Configurer.EnableService(h, p.Distro.GetWorkerService())
}
