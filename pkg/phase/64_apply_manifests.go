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

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// applyManifestCmd is the kubectl subcommand template ApplyManifests runs for each manifest path.
const applyManifestCmd = `apply -f %s`

// ApplyManifests kubectl-applies the raw manifests a distro's package declares -- typically a CNI
// -- from the leader, once it has bootstrapped and before workers are expected to join.
type ApplyManifests struct {
	GenericPhase
	Distro distrocfg.Distro
	leader *cluster.ZarfHost
	paths  []string
}

// manifestsToApply returns the manifest paths d wants applied, or nil when d does not implement
// ManifestApplier -- rke2/k3s render engine.manifest into a HelmChartConfig instead and never
// declare any.
func manifestsToApply(d distrocfg.Distro, dis distro.ZarfDistro) []string {
	if a, ok := d.(distrocfg.ManifestApplier); ok {
		return a.ManifestPaths(dis)
	}
	return nil
}

// Title for the phase
func (p *ApplyManifests) Title() string {
	return "Apply manifests"
}

// Explanation about the current phase, used for documentation generation
func (p *ApplyManifests) Explanation() string {
	return "For a distro whose package declares raw manifests -- a CNI, typically -- kubectl applies each one from the leader"
}

// Prepare the phase
func (p *ApplyManifests) Prepare(ctx context.Context, c *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	if err := p.GenericPhase.Prepare(c, d); err != nil {
		logger.From(ctx).Warn("got", "error", err)
	}
	control := p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool { return h.IsController() })
	if len(control) > 0 {
		p.leader = control[0]
	}
	p.paths = manifestsToApply(p.Distro, *p.GetDistro())
	return nil
}

// ShouldRun is true when there is a leader to apply from and the package declares manifests.
func (p *ApplyManifests) ShouldRun() bool {
	return p.leader != nil && len(p.paths) > 0
}

// Run the phase
func (p *ApplyManifests) Run(ctx context.Context) error {
	for _, path := range p.paths {
		path := path
		logger.From(ctx).Info("applying manifest", "path", path, "host", p.leader)
		if err := p.manager.RetryTimeout(ctx, func(_ context.Context) error {
			return p.leader.Sudo().Exec(p.Distro.KubectlCmdf(p.leader, p.Distro.DataDirPath(), applyManifestCmd, path))
		}); err != nil {
			return fmt.Errorf("applying manifest %s: %w", path, err)
		}
	}
	return nil
}
