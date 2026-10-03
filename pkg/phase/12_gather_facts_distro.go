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
	"fmt"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	// UnknownVersion for when the version is not set
	UnknownVersion = "v0.0.0"
)

// ErrWillNotDowngrade is returned when a host already runs a version newer than the one the
// package carries. An engine does not support moving backwards, so the run stops here rather than
// uninstalling a newer version part way through.
var ErrWillNotDowngrade = errors.New("will not downgrade the cluster: raise the package version, or pass --allow-downgrade to continue anyway")

// GatherFactsDistro state
type GatherFactsDistro struct {
	GenericPhase
	Distro distrocfg.Distro
	// AllowDowngrade turns the refusal below into a warning. A downgrade is refused by default
	// because an engine does not support being moved backwards and the data directory it leaves
	// behind was written by the newer version, so the operator has to say that they mean it.
	AllowDowngrade bool
	hosts          cluster.ZarfHosts
	d              *distro.ZarfDistro
}

// Title for the phase
func (p *GatherFactsDistro) Title() string {
	return "Gathering facts about the distro installed"
}

// Explanation about the current phase, used for documentation generation
func (p *GatherFactsDistro) Explanation() string {
	return "Gathers information relating to the specific distro being installed, including: if the distro is installed, and what version it is running"
}

// ReadOnly marks this phase safe under a dry run, and returns the reason for the phase docs.
func (p *GatherFactsDistro) ReadOnly() string {
	return "Gather distro related facts reads the engine version already on each host. That is what tells an upgrade apart from an install, and what catches a downgrade before a real run starts one."
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
	ver, err := p.Distro.RunningVersion(h)
	if err != nil && !errors.Is(err, distrocfg.ErrVersionNotDetected) {
		return err
	} else if errors.Is(err, distrocfg.ErrVersionNotDetected) {
		h.Metadata.DistroVersion = UnknownVersion
	} else {
		h.Metadata.DistroVersion = ver
	}
	logger.From(ctx).Info("detected", "host", h, "version", h.Metadata.DistroVersion)
	if p.d != nil && p.VersionGreater(h, p.d.Spec.Version) {
		if p.AllowDowngrade {
			logger.From(ctx).Warn(
				"the host runs a version newer than the package, and this run was told to continue anyway",
				"host", h,
				"running", h.Metadata.DistroVersion,
				"package", p.d.Spec.Version,
			)
			return nil
		}
		// Named rather than bare: the refusal is read by somebody who has to work out which host
		// and which version, and this phase runs before anything has written to a host, so the
		// message is the entire output of the run.
		return fmt.Errorf("%w: %s runs %s, the package carries %s", ErrWillNotDowngrade, h, h.Metadata.DistroVersion, p.d.Spec.Version)
	}
	return nil
}
