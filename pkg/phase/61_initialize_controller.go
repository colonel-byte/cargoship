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

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// InitializeControllers phase state
type InitializeControllers struct {
	GenericPhase
	Distro  distrocfg.Distro
	run     cluster.RuntimeMeta
	control cluster.Hosts
}

// controllersNeedingInit filters hosts down to controllers that have not yet formed or joined the
// cluster. When d implements Bootstrapper (kubeadm-style one-shot cluster formation, no service to
// poll) that is asked directly; every other distro falls back to the existing service-running plus
// UnknownVersion check.
func controllersNeedingInit(ctx context.Context, hosts cluster.Hosts, d distrocfg.Distro) cluster.Hosts {
	return hosts.Filter(func(h *cluster.Host) bool {
		if !h.IsController() {
			return false
		}
		if b, ok := d.(distrocfg.Bootstrapper); ok {
			return !b.IsBootstrapped(h)
		}
		return !h.ServiceIsRunning(ctx, d.GetControllerService()) && h.Metadata.DistroVersion == UnknownVersion
	})
}

// Prepare the phase
func (p *InitializeControllers) Prepare(ctx context.Context, c *cluster.Cluster, _ *distro.Distro) error {
	control := p.manager.Config.Spec.Hosts.Filter(func(h *cluster.Host) bool { return h.IsController() })
	// Recomputed independently of ConfigureEngine.Prepare's identical Leader/LoadBalancer setup --
	// deliberate, not DRY-able, so this phase does not depend on phase-ordering assumptions.
	if len(control) > 0 {
		control[0].Metadata.IsLeader = true
		p.run.Leader = control[0]
	}
	p.run.LoadBalancer = c.Spec.Config.LoadBalancer

	p.control = controllersNeedingInit(ctx, p.manager.Config.Spec.Hosts, p.Distro)

	logger.From(ctx).Debug("number of systems that need to be started", "hosts", len(p.control))

	return nil
}

// Title for the phase
func (p *InitializeControllers) Title() string {
	return "Initialize Controller"
}

// Explanation about the current phase, used for documentation generation
func (p *InitializeControllers) Explanation() string {
	return "If the remote node does not have a running controller service, and is a controller, install the engine and start each service sequentially"
}

// Run the phase
func (p *InitializeControllers) Run(ctx context.Context) error {
	err := p.parallelDoWithMessage(
		ctx,
		"installing distro engine",
		p.control,
		p.installDistro,
	)
	if err != nil {
		return err
	}
	// waiting a second too clean up the logs
	time.Sleep(1 * time.Second)

	if b, ok := p.Distro.(distrocfg.Bootstrapper); ok {
		return p.batchedParallelWithMessage(
			ctx,
			"bootstrapping controller",
			p.control,
			1,
			func(ctx context.Context, h *cluster.Host) error {
				return b.Bootstrap(ctx, h, p.run, *p.GetDistro())
			},
		)
	}
	return p.batchedParallelWithMessage(
		ctx,
		"starting engine",
		p.control,
		1,
		p.startService,
	)
}

// ShouldRun is true when there are workers
func (p *InitializeControllers) ShouldRun() bool {
	return len(p.control) > 0
}

func (p *InitializeControllers) installDistro(ctx context.Context, h *cluster.Host) error {
	return p.installPackages(ctx, h)
}

func (p *InitializeControllers) startService(ctx context.Context, h *cluster.Host) error {
	service := p.Distro.GetControllerService()
	logger.From(ctx).Info("waiting for the controller service to start", "service", service, "host", h)
	return p.startAndEnableService(ctx, h, service)
}
