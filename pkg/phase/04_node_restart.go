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
	"github.com/colonel-byte/cargoship/pkg/node"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// Four phases take a node out of the cluster, change it, and put it back: the two initialize
// phases start a service on a fresh host, and the upgrade and config-sync phases drain, restart
// and wait for ready. The steps in the middle were written out once per phase, and the copies
// drifted in their log lines while staying identical in the parts that are easy to get wrong --
// the fire-and-wait around systemd, the readiness probe's string comparison. They live here for
// the same reason resolveNodeNames does, one file over: so the next phase that needs one does
// not learn it a fifth time.
//
// Each caller keeps its own log line. What is shared is the mechanism, not what it says.

// nodeNameCache resolves what the cluster calls each host's node, once per phase.
//
// The lookup is deferred rather than done in Prepare because it costs a kubectl call on the
// leader, and a phase with nothing to do should not make one. Resolving it on first use rather
// than per host keeps that at one call for the whole phase however many hosts it touches.
type nodeNameCache struct {
	names map[string]string
	once  sync.Once
}

// nameFor returns the name h's node carries in the cluster, or "" when it has none.
func (c *nodeNameCache) nameFor(ctx context.Context, leader *cluster.Host, d distrocfg.Distro, hosts cluster.Hosts, h *cluster.Host) string {
	c.once.Do(func() {
		c.names = resolveNodeNames(ctx, leader, d, hosts)
	})
	return nodeNameFor(c.names, h)
}

// startAndEnableService starts service on h, waits for it to report running, and enables it.
//
// The start is fired into a goroutine and the outcome is read from the retry rather than from
// StartService: a unit that takes minutes to settle makes systemd's own start block for the
// whole of it, and a failure there says only that the start timed out. The retry on
// ServiceRunningFunc polls instead, which is what lets captureServiceLogsOnFailure attach the
// unit's own log from startedAt when it never comes up.
func (p *GenericPhase) startAndEnableService(ctx context.Context, h *cluster.Host, service string) error {
	startedAt := time.Now()
	go func() {
		if err := h.StartService(ctx, service); err != nil {
			logger.From(ctx).Warn("failed to start", "service", service, "host", h)
		}
	}()

	if err := p.manager.RetryTimeout(ctx, node.ServiceRunningFunc(h, service)); err != nil {
		return p.captureServiceLogsOnFailure(ctx, h, service, startedAt, err)
	}

	return h.EnableService(ctx, service)
}

// drainClusterNode drains nodeName through the leader, retrying for the manager's timeout.
func (p *GenericPhase) drainClusterNode(ctx context.Context, leader *cluster.Host, d distrocfg.Distro, nodeName string) error {
	return p.manager.RetryTimeout(ctx, func(_ context.Context) error {
		return leader.Sudo().Exec(d.KubectlCmdf(leader, d.DataDirPath(), drainNode, nodeName))
	})
}

// waitNodeReady blocks until the cluster reports nodeName's Ready condition true, or the
// manager's timeout runs out.
func (p *GenericPhase) waitNodeReady(ctx context.Context, leader *cluster.Host, d distrocfg.Distro, nodeName string) error {
	return p.manager.RetryTimeout(ctx, func(_ context.Context) error {
		out, err := leader.Sudo().ExecOutput(d.KubectlCmdf(leader, d.DataDirPath(), readyNode, nodeName))
		if err != nil {
			return err
		}
		if strings.ToLower(out) != "true" {
			return errors.New("node not ready")
		}
		return nil
	})
}

// uncordonClusterNode puts nodeName back into scheduling. No retry: it is the last step of a
// walk whose earlier steps already established that the leader answers.
func uncordonClusterNode(leader *cluster.Host, d distrocfg.Distro, nodeName string) error {
	return leader.Sudo().Exec(d.KubectlCmdf(leader, d.DataDirPath(), uncordonNode, nodeName))
}

// installPackages runs the install closure the package staged on h, when it staged one.
//
// The closure is cleared once it has run, so it runs at most once per host. That is what lets
// InstallStagedFiles tell a host whose install nothing ran from one that is already installed:
// the initialize and upgrade phases are each gated on having a host to act on, and a converge of
// a cluster already at the package's version runs neither -- see InstallStagedFiles for what that
// left behind.
func (p *GenericPhase) installPackages(ctx context.Context, h *cluster.Host) error {
	install := h.Metadata.Install
	if install == nil {
		return nil
	}
	h.Metadata.Install = nil
	return install(ctx, h)
}
