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
	"github.com/k0sproject/rig/exec"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	deleteNode = `delete node %s`
)

// DeleteWorkers phase state
type DeleteWorkers struct {
	GenericPhase
	Distro           distrocfg.Distro
	NoDrain          bool
	WorkerConcurrent int
	hosts            cluster.ZarfHosts
	leader           *cluster.ZarfHost
	service          string
}

// Title for the phase
func (p *DeleteWorkers) Title() string {
	return "Reset Worker"
}

func (p *DeleteWorkers) Explanation() string {
	return ""
}

// Prepare the phase
func (p *DeleteWorkers) Prepare(ctx context.Context, _ *cluster.ZarfCluster, _ *distro.ZarfDistro) error {
	control := p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return h.Configurer.ServiceIsRunning(h, p.Distro.GetControllerService()) && h.IsController()
	})
	p.leader = control[0]
	p.hosts = p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return !h.IsController()
	})
	logger.From(ctx).Debug("number of systems that need to be reset", "hosts", len(p.hosts))
	p.service = p.Distro.GetWorkerService()

	return nil
}

// Run the phase
func (p *DeleteWorkers) Run(ctx context.Context) error {
	if !p.NoDrain {
		err := p.batchedParallelWithMessage(
			ctx,
			"draining nodes",
			p.hosts,
			p.WorkerConcurrent,
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
		p.WorkerConcurrent,
		p.deleteNode,
	)
}

func (p *DeleteWorkers) drainNode(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("draining", "node", h)
	return p.manager.RetryTimeout(ctx, func(_ context.Context) error {
		return p.leader.Exec(p.Distro.KubectlCmdf(*p.leader, p.Distro.DataDirPath(), drainNode, h.Configurer.Hostname(h)), exec.Sudo(p.leader))
	})
}

func (p *DeleteWorkers) deleteNode(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("deleting", "node", h)
	return p.manager.RetryTimeout(ctx, func(_ context.Context) error {
		return p.leader.Exec(p.Distro.KubectlCmdf(*p.leader, p.Distro.DataDirPath(), deleteNode, h.Configurer.Hostname(h)), exec.Sudo(p.leader))
	})
}
