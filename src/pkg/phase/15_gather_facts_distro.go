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

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/types/distrocfg"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	// UnknownVersion for when the version is not set
	UnknownVersion = "v0.0.0"
)

// GatherFactsDistro state
type GatherFactsDistro struct {
	GenericPhase
	Distro distrocfg.Distro
	hosts  cluster.ZarfHosts
	d      *distro.ZarfDistro
}

// Title for the phase
func (p *GatherFactsDistro) Title() string {
	return "Gathering facts about the distro installed"
}

// Prepare the phase
func (p *GatherFactsDistro) Prepare(_ context.Context, _ *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	p.hosts = p.manager.Config.Spec.Hosts
	p.d = d
	return nil
}

// Run the phase
func (p *GatherFactsDistro) Run(ctx context.Context) (err error) {
	return p.parallelDoUpload(
		ctx,
		p.hosts,
		p.investigateHostDistro,
	)
}

func (p *GatherFactsDistro) investigateHostDistro(ctx context.Context, h *cluster.ZarfHost) error {
	ver, err := p.Distro.RunningVersion(*h)
	if err != nil && !errors.Is(err, distrocfg.ErrVersionNotDetected) {
		return err
	} else if errors.Is(err, distrocfg.ErrVersionNotDetected) {
		h.Metadata.DistroVersion = UnknownVersion
	} else {
		h.Metadata.DistroVersion = ver
	}
	logger.From(ctx).Info("detected", "host", h, "version", h.Metadata.DistroVersion)
	if p.VersionGreater(h, p.d.Spec.Version) {
		return errors.New("will not downgrade the cluster")
	}
	return nil
}
