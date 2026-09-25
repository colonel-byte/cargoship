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
	"time"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/pkg/retry"
	"github.com/colonel-byte/cargoship/src/types/distrocfg"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	deleteNode = `delete node %s`
	getNode    = `get node %s --no-headers 2>/dev/null`
)

// DeleteCommon phase state
type DeleteCommon struct {
	GenericPhase
	Distro distrocfg.Distro
	leader *cluster.ZarfHost
}

// Prepare the phase
func (p *DeleteCommon) Prepare(ctx context.Context, _ *cluster.ZarfCluster, _ *distro.ZarfDistro) error {
	control := p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return h.ServiceIsRunning(ctx, p.Distro.GetControllerService()) && h.IsController()
	})
	if len(control) > 0 {
		p.leader = control[0]
	} else {
		logger.From(ctx).Warn("there is no running controllers")
	}
	return nil
}

func (p *DeleteCommon) drainNode(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("draining", "node", h)
	return p.manager.RetryTimeout(ctx, func(_ context.Context) error {
		return p.leader.Sudo().Exec(p.Distro.KubectlCmdf(p.leader, p.Distro.DataDirPath(), drainNode, h.Configurer.Hostname(h)))
	})
}

func (p *DeleteCommon) deleteNode(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("deleting", "node", h)
	err := retry.Timeout(ctx, 10*time.Second, func(_ context.Context) error {
		return p.leader.Sudo().Exec(p.Distro.KubectlCmdf(p.leader, p.Distro.DataDirPath(), deleteNode, h.Configurer.Hostname(h)))
	})
	if err != nil {
		logger.From(ctx).Warn("got an error well deleting the", "node", h.Configurer.Hostname(h))
	}
	return nil
}
