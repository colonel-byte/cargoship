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
	"sync"
	"time"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/pkg/node"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	drainNode    = `drain --delete-emptydir-data --ignore-daemonsets --disable-eviction %s`
	uncordonNode = `uncordon %s`
	readyNode    = `get node -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' %s`
)

// UpgradeHosts phase state
type UpgradeHosts struct {
	GenericPhase
	Distro  distrocfg.Distro
	service string
	hosts   cluster.Hosts
	leader  *cluster.Host
	dis     *distro.Distro
	// nodeNames maps a host to the name its node carries in the cluster. See nodeName.
	nodeNames     map[string]string
	nodeNamesOnce sync.Once
}

// nodeName is what the cluster calls h's node, resolved once for the whole phase the first time
// a node has to be named. Resolved here rather than in Prepare because it costs a kubectl call
// on the leader, and a phase with nothing to upgrade should not make one.
func (p *UpgradeHosts) nodeName(ctx context.Context, h *cluster.Host) string {
	p.nodeNamesOnce.Do(func() {
		p.nodeNames = resolveNodeNames(ctx, p.leader, p.Distro, p.hosts)
	})
	return nodeNameFor(p.nodeNames, h)
}

// ShouldRun is true when there are workers
func (p *UpgradeHosts) ShouldRun() bool {
	return len(p.hosts) > 0
}

func (p *UpgradeHosts) drainNode(ctx context.Context, h *cluster.Host) error {
	logger.From(ctx).Info("draining nodes", "node", h)
	return p.manager.RetryTimeout(ctx, func(_ context.Context) error {
		return p.leader.Sudo().Exec(p.Distro.KubectlCmdf(p.leader, p.Distro.DataDirPath(), drainNode, p.nodeName(ctx, h)))
	})
}

func (p *UpgradeHosts) installDistro(ctx context.Context, h *cluster.Host) error {
	logger.From(ctx).Info("installing distro packages", "host", h)
	if h.Metadata.Install != nil {
		return h.Metadata.Install(ctx, h)
	}
	return nil
}

// preStartUpgrade runs the distro's kubeadm-style upgrade step, when it implements one, after the
// new packages are staged but before the service restarts. A no-op for rke2/k3s, whose plain
// restart on the new binary needs nothing in between.
func (p *UpgradeHosts) preStartUpgrade(ctx context.Context, h *cluster.Host) error {
	u, ok := p.Distro.(distrocfg.PreStartUpgrader)
	if !ok {
		return nil
	}
	logger.From(ctx).Info("running pre-start upgrade step", "host", h)
	return u.PreStartUpgrade(ctx, h, *p.dis)
}

func (p *UpgradeHosts) startService(ctx context.Context, h *cluster.Host) error {
	logger.From(ctx).Info("waiting for the service to start", "service", p.service, "host", h)

	startedAt := time.Now()
	go func() {
		err := h.StartService(ctx, p.service)
		if err != nil {
			logger.From(ctx).Warn("failed to start", "service", p.service, "host", h)
		}
	}()

	if err := p.manager.RetryTimeout(ctx, node.ServiceRunningFunc(h, p.service)); err != nil {
		return p.captureServiceLogsOnFailure(ctx, h, p.service, startedAt, err)
	}

	return h.EnableService(ctx, p.service)
}

func (p *UpgradeHosts) waitForNodeReady(ctx context.Context, h *cluster.Host) error {
	logger.From(ctx).Info("waiting for the node to be in a ready state", "host", h)

	return p.manager.RetryTimeout(ctx, func(_ context.Context) error {
		out, err := p.leader.Sudo().ExecOutput(p.Distro.KubectlCmdf(p.leader, p.Distro.DataDirPath(), readyNode, p.nodeName(ctx, h)))
		if err != nil {
			return err
		}
		if strings.ToLower(out) != "true" {
			return errors.New("node not ready")
		}
		return nil
	})
}

func (p *UpgradeHosts) uncordonNode(ctx context.Context, h *cluster.Host) error {
	return p.leader.Sudo().Exec(p.Distro.KubectlCmdf(p.leader, p.Distro.DataDirPath(), uncordonNode, p.nodeName(ctx, h)))
}
