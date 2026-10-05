// Copyright 2023 k0sctl authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from k0sctl:
// https://github.com/k0sproject/k0sctl
//
// Modifications Copyright 2026 colonel-byte.
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
	"time"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/pkg/node"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// InitializeWorkers phase state
type InitializeWorkers struct {
	GenericPhase
	Distro           distrocfg.Distro
	run              cluster.ZarfRuntimeMeta
	worker           cluster.ZarfHosts
	WorkerConcurrent string
}

// workersNeedingInit filters hosts down to non-controllers that have not yet joined the cluster.
// When d implements Bootstrapper that is asked directly; every other distro falls back to the
// existing service-running check.
func workersNeedingInit(ctx context.Context, hosts cluster.ZarfHosts, d distrocfg.Distro) cluster.ZarfHosts {
	return hosts.Filter(func(h *cluster.ZarfHost) bool {
		if h.IsController() {
			return false
		}
		if b, ok := d.(distrocfg.Bootstrapper); ok {
			return !b.IsBootstrapped(h)
		}
		return !h.ServiceIsRunning(ctx, d.GetWorkerService())
	})
}

// Title for the phase
func (p *InitializeWorkers) Title() string {
	return "Initialize Worker"
}

// Explanation about the current phase, used for documentation generation
func (p *InitializeWorkers) Explanation() string {
	return "If the remote node does not have a running worker service, and is not a controller, install the engine and start each service by the set concurrency limit"
}

// Prepare the phase
func (p *InitializeWorkers) Prepare(ctx context.Context, c *cluster.ZarfCluster, _ *distro.ZarfDistro) error {
	control := p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool { return h.IsController() })
	// Recomputed independently of ConfigureEngine.Prepare's identical Leader/LoadBalancer setup --
	// deliberate, not DRY-able, so this phase does not depend on phase-ordering assumptions.
	if len(control) > 0 {
		control[0].Metadata.IsLeader = true
		p.run.Leader = control[0]
	}
	p.run.LoadBalancer = c.Spec.Config.LoadBalancer

	p.worker = workersNeedingInit(ctx, p.manager.Config.Spec.Hosts, p.Distro)
	logger.From(ctx).Debug("number of systems that need to be started", "hosts", len(p.worker))

	return nil
}

// ShouldRun is true when there are workers
func (p *InitializeWorkers) ShouldRun() bool {
	return len(p.worker) > 0
}

// Run the phase
func (p *InitializeWorkers) Run(ctx context.Context) error {
	err := p.parallelDoWithMessage(
		ctx,
		"installing distro engine",
		p.worker,
		p.installDistro,
	)
	if err != nil {
		return err
	}
	// waiting a second too clean up the logs
	time.Sleep(1 * time.Second)

	if b, ok := p.Distro.(distrocfg.Bootstrapper); ok {
		return p.batchedParallelPerProfileWithMessage(
			ctx,
			"joining cluster",
			p.worker,
			p.WorkerConcurrent,
			func(ctx context.Context, h *cluster.ZarfHost) error {
				return b.Bootstrap(ctx, h, p.run, *p.GetDistro())
			},
		)
	}
	return p.batchedParallelPerProfileWithMessage(
		ctx,
		"starting agent",
		p.worker,
		p.WorkerConcurrent,
		p.startService,
	)
}

func (p *InitializeWorkers) installDistro(ctx context.Context, h *cluster.ZarfHost) error {
	if h.Metadata.Install != nil {
		return h.Metadata.Install(ctx, h)
	}
	return nil
}

func (p *InitializeWorkers) startService(ctx context.Context, h *cluster.ZarfHost) error {
	service := p.Distro.GetWorkerService()
	logger.From(ctx).Info("waiting for the worker service to start", "service", service, "host", h)

	startedAt := time.Now()
	go func() {
		err := h.StartService(ctx, service)
		if err != nil {
			logger.From(ctx).Warn("failed to start", "service", service, "host", h)
		}
	}()

	if err := p.manager.RetryTimeout(ctx, node.ServiceRunningFunc(h, service)); err != nil {
		return p.captureServiceLogsOnFailure(ctx, h, service, startedAt, err)
	}

	return h.EnableService(ctx, service)
}
