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
	"fmt"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// PrepareKernelModules enables the list of requested modules on the host, then reboots the box if modules are added
type PrepareKernelModules struct {
	GenericPhase
	Enabled bool
	modules []string
}

// Title for the phase
func (p *PrepareKernelModules) Title() string {
	return "Enable the requested kernel modueles"
}

// Explanation about the current phase, used for documentation generation
func (p *PrepareKernelModules) Explanation() string {
	return "Turns on the list of requested modules on the host, then reboots the box if modules are added"
}

// Prepare the phase
func (p *PrepareKernelModules) Prepare(_ context.Context, _ *cluster.ZarfCluster, _ *distro.ZarfDistro) error {
	p.modules = p.manager.Distro.Spec.Config.OS.Kernel
	return nil
}

// ShouldRun is true when we need to enable kernel modules
func (p *PrepareKernelModules) ShouldRun() bool {
	return p.Enabled && len(p.modules) > 0
}

// Run the phase
func (p *PrepareKernelModules) Run(ctx context.Context) error {
	return p.batchedParallelWithMessage(
		ctx,
		"enabling kernel modules",
		p.manager.Config.Spec.Hosts,
		1,
		p.enableModules,
		p.rebootNodes,
	)
}

func (p *PrepareKernelModules) enableModules(ctx context.Context, h *cluster.ZarfHost) error {
	for _, m := range p.modules {
		load := fmt.Sprintf("/etc/modules-load.d/%s.conf", m)
		if !h.Configurer.FileExist(h, load) {
			h.Metadata.ModulesAdded = true
			logger.From(ctx).Info("enabling kernel module", "host", h, "modules", m)
			if err := h.Configurer.WriteFile(h, load, m, "0600"); err != nil {
				logger.From(ctx).Warn("could not write", "host", h, "modules", m, "file", load)
			}
		}
	}
	return nil
}

func (p *PrepareKernelModules) rebootNodes(ctx context.Context, h *cluster.ZarfHost) error {
	if h.Metadata.ModulesAdded {
		if err := h.Configurer.Reboot(h); err != nil {
			logger.From(ctx).Warn("issue when trying to reboot", "host", h)
		}
	}
	return nil
}
