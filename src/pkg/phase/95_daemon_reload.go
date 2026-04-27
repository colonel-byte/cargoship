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
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// DaemonReload phase runs `systemctl daemon-reload` or equivalent on all hosts.
type DaemonReload struct {
	GenericPhase
}

// Title for the phase
func (p *DaemonReload) Title() string {
	return "Reload service manager"
}

// Explanation about the current phase, used for documentation generation
func (p *DaemonReload) Explanation() string {
	return "Runs `systemctl daemon-reload` or equivalent on all hosts."
}

// ShouldRun is true when there are controllers that needs to be reset
func (p *DaemonReload) ShouldRun() bool {
	return len(p.manager.Config.Spec.Hosts) > 0
}

// Run the phase
func (p *DaemonReload) Run(ctx context.Context) error {
	return p.parallelDo(ctx, p.manager.Config.Spec.Hosts, func(_ context.Context, h *cluster.ZarfHost) error {
		logger.From(ctx).Info("reloading service manager", "host", h)
		if err := h.Configurer.DaemonReload(h); err != nil {
			logger.From(ctx).Warn("failed to reload service manager", "host", h, "error", err)
		}
		return nil
	})
}
