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

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// ImportImages imports uploaded image tarballs into a distro's engine, for a distro whose engine
// does not do this on its own. rke2 and k3s's own agents import automatically, so most applies
// skip this phase entirely -- ShouldRun is false unless the distro opts in.
type ImportImages struct {
	GenericPhase
	Distro distrocfg.Distro
	hosts  cluster.ZarfHosts
	path   string
}

// Title for the phase
func (p *ImportImages) Title() string {
	return "Import images"
}

// Explanation about the current phase, used for documentation generation
func (p *ImportImages) Explanation() string {
	return "For a distro whose engine does not import uploaded image tarballs on its own, imports them into the engine's image store"
}

// Prepare the phase
func (p *ImportImages) Prepare(ctx context.Context, c *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	if err := p.GenericPhase.Prepare(c, d); err != nil {
		logger.From(ctx).Warn("got", "error", err)
	}
	p.hosts = p.manager.Config.Spec.Hosts
	p.path = p.manager.Distro.Spec.Config.ImagesConfig.Path
	return nil
}

// ShouldRun is true when the distro needs images imported for it and there is somewhere to
// import them from.
func (p *ImportImages) ShouldRun() bool {
	_, ok := p.Distro.(distrocfg.ImageImporter)
	return ok && p.path != "" && len(p.hosts) > 0
}

// Run the phase
func (p *ImportImages) Run(ctx context.Context) error {
	importer := p.Distro.(distrocfg.ImageImporter) //nolint:errcheck
	return p.parallelDo(ctx, p.hosts, func(_ context.Context, h *cluster.ZarfHost) error {
		return importer.ImportImages(h, p.path)
	})
}
