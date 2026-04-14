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
	"github.com/colonel-byte/zarf-distro/src/pkg/utils"
	tdis "github.com/colonel-byte/zarf-distro/src/types/distro"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// ConfigureEngine writes the k0s configuration to host k0s config dir
type ConfigureEngine struct {
	GenericPhase
	Distro  tdis.Distro
	run     cluster.ZarfRuntimeMeta
	leader  *cluster.ZarfHost
	hosts   cluster.ZarfHosts
	control cluster.ZarfHosts
}

// Prepare the phase
func (p *ConfigureEngine) Prepare(ctx context.Context, c *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	p.GenericPhase.Prepare(c, d)
	p.hosts = p.manager.Config.Spec.Hosts
	p.control = p.hosts.Filter(func(h *cluster.ZarfHost) bool {
		return h.IsController()
	})

	p.run = p.manager.Config.RuntimeMetadata

	if len(p.control) > 0 {
		p.control[0].Metadata.IsLeader = true
		p.run.Leader = p.control[0]
	}

	p.run.ControllerTLS = append(p.run.ControllerTLS, c.Spec.Config.LoadBalancer)
	p.run.LoadBalancer = c.Spec.Config.LoadBalancer

	for _, h := range p.control {
		p.run.ControllerTLS = append(p.run.ControllerTLS, h.Configurer.Hostname(h))
		p.run.ControllerTLS = append(p.run.ControllerTLS, h.Configurer.LongHostname(h))
	}

	if token, err := utils.RandomString(64); err == nil {
		p.run.ControllerToken = token
	} else {
		logger.From(ctx).Warn("failed to read random", "error", err)
		return err
	}

	if token, err := utils.RandomString(64); err == nil {
		p.run.AgentToken = token
	} else {
		logger.From(ctx).Warn("failed to read random", "error", err)
		return err
	}

	return nil
}

// Title returns the phase title
func (p *ConfigureEngine) Title() string {
	return "Configure engine"
}

func (p *ConfigureEngine) Run(ctx context.Context) error {
	con := p.manager.Concurrency
	p.manager.Concurrency = 1
	if err := p.parallelDo(ctx, p.hosts, p.configureEngine); err != nil {
		return err
	}
	p.manager.Concurrency = con
	return nil
}

func (p *ConfigureEngine) configureEngine(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("applying config", "host", h)
	return p.Distro.ConfigureEngine(ctx, *h, p.run, *p.GetDistro())
}
