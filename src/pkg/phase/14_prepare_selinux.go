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

const (
	// ContainerSELinux package name
	ContainerSELinux = "container-selinux"
)

// PrepareSelinux installs required packages and so on on the hosts.
type PrepareSelinux struct {
	GenericPhase
	selinuxHosts cluster.ZarfHosts
}

// Prepare the phase
func (p *PrepareSelinux) Prepare(ctx context.Context, _ *cluster.ZarfCluster, _ *distro.ZarfDistro) error {
	p.selinuxHosts = p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return h.Configurer.SELinuxEnabled(h)
	})

	logger.From(ctx).Info("number of systems with selinux", "hosts", len(p.selinuxHosts))

	return nil
}

// Title for the phase
func (p *PrepareSelinux) Title() string {
	return "Prepare hosts - Enterprise Linux support"
}

// Explanation about the current phase, used for documentation generation
func (p *PrepareSelinux) Explanation() string {
	return fmt.Sprintf(`Installs %s on systems that have SELinux enabled on them`, ContainerSELinux)
}

// Run the phase
func (p *PrepareSelinux) Run(ctx context.Context) error {
	return p.parallelDoWithMessage(ctx, "waiting for package to install", p.selinuxHosts, p.prepareHost)
}

// ShouldRun is true when there is a host with selinux on the hosts
func (p *PrepareSelinux) ShouldRun() bool {
	return len(p.selinuxHosts) > 0
}

func (p *PrepareSelinux) prepareHost(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("attempting to install", "host", h, "package", ContainerSELinux)
	err := h.Configurer.InstallPackage(h, ContainerSELinux)
	if err != nil {
		logger.From(ctx).Warn("could not install", "host", h, "package", ContainerSELinux, "error", err)
		return err
	}
	return nil
}
