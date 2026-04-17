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

type InitializeControllers struct {
	GenericPhase
	Distro  distrocfg.Distro
	control cluster.ZarfHosts
}

// Prepare the phase
func (p *InitializeControllers) Prepare(ctx context.Context, c *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	p.control = p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return !h.Configurer.ServiceIsRunning(h, p.Distro.GetControllerService()) && h.IsController()
	})

	logger.From(ctx).Info("number of systems that need to be started", "hosts", len(p.control))

	return nil
}

func (p *InitializeControllers) Title() string {
	return "Initialize Controller"
}

func (p *InitializeControllers) Run(ctx context.Context) error {
	return p.control.BatchedParallelEach(
		ctx,
		1,
		p.startService,
		p.enableService,
	)
}

// ShouldRun is true when there are workers
func (p *InitializeControllers) ShouldRun() bool {
	return len(p.control) > 0
}

func (p *InitializeControllers) startService(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("waiting for the controller service to start", "service", p.Distro.GetControllerService(), "host", h)
	return h.Configurer.StartService(h, p.Distro.GetControllerService())
}

func (p *InitializeControllers) enableService(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("enabling the controller service", "service", p.Distro.GetControllerService(), "host", h)
	return h.Configurer.EnableService(h, p.Distro.GetWorkerService())
}
