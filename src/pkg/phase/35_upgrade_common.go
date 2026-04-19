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
	"errors"
	"strings"

	"github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/zarf-distro/src/pkg/node"
	"github.com/colonel-byte/zarf-distro/src/pkg/retry"
	"github.com/colonel-byte/zarf-distro/src/types/distrocfg"
	"github.com/k0sproject/rig/exec"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	drain_node    = `drain --delete-emptydir-data --ignore-daemonsets --disable-eviction %s`
	uncordon_node = `uncordon %s`
	ready_node    = `get node -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' %s`
)

type UpgradeHosts struct {
	GenericPhase
	Distro  distrocfg.Distro
	service string
	hosts   cluster.ZarfHosts
	leader  *cluster.ZarfHost
}

// ShouldRun is true when there are workers
func (p *UpgradeHosts) ShouldRun() bool {
	return len(p.hosts) > 0
}

func (p *UpgradeHosts) drainNode(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("draining nodes", "node", h)
	return p.leader.Exec(p.Distro.KubectlCmdf(*p.leader, p.Distro.DataDirPath(), drain_node, h.Configurer.Hostname(h)), exec.Sudo(p.leader))
}

func (p *UpgradeHosts) stopService(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("waiting for the service to stop", "service", p.service, "host", h)
	return h.Configurer.StopService(h, p.service)
}

func (p *UpgradeHosts) installDistro(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("installing distro packages", "host", h)
	if h.Metadata.Install != nil {
		return h.Metadata.Install(ctx, h)
	}
	return nil
}

func (p *UpgradeHosts) startService(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("waiting for the service to start", "service", p.service, "host", h)

	go func() {
		h.Configurer.StartService(h, p.service)
	}()

	if err := retry.WithDefaultTimeout(ctx, node.ServiceRunningFunc(h, p.service)); err != nil {
		return err
	}

	return h.Configurer.EnableService(h, p.service)
}

func (p *UpgradeHosts) waitForNodeReady(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("waiting for the node to be in a ready state", "host", h)

	return retry.WithDefaultTimeout(ctx, func(ctx context.Context) error {
		out, err := p.leader.ExecOutput(p.Distro.KubectlCmdf(*p.leader, p.Distro.DataDirPath(), ready_node, h.Configurer.Hostname(h)), exec.Sudo(p.leader))
		if err != nil {
			return err
		}
		if strings.ToLower(out) != "true" {
			return errors.New("node not ready")
		}
		return nil
	})
}

func (p *UpgradeHosts) uncordonNode(ctx context.Context, h *cluster.ZarfHost) error {
	return p.leader.Exec(p.Distro.KubectlCmdf(*p.leader, p.Distro.DataDirPath(), uncordon_node, h.Configurer.Hostname(h)), exec.Sudo(p.leader))
}
