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
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	CONTAINER_SELINUX = "container-selinux"
)

// PrepareHosts installs required packages and so on on the hosts.
type PrepareSelinux struct {
	GenericPhase
	selinuxHosts cluster.ZarfHosts
}

// Prepare the phase
func (p *PrepareSelinux) Prepare(ctx context.Context, c *cluster.ZarfCluster, d *distro.ZarfDistro) error {
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

// Run the phase
func (p *PrepareSelinux) Run(ctx context.Context) error {
	return p.parallelDoWithMessage(ctx, "waiting for package to install", p.selinuxHosts, p.prepareHost)
}

// ShouldRun is true when there is a host with selinux or fapolicyd on the hosts
func (p *PrepareSelinux) ShouldRun() bool {
	return len(p.selinuxHosts) > 0
}

func (p *PrepareSelinux) prepareHost(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("attempting to install", "host", h, "package", CONTAINER_SELINUX)
	err := h.Configurer.InstallPackage(h, CONTAINER_SELINUX)
	if err != nil {
		logger.From(ctx).Warn("could not install", "host", h, "package", CONTAINER_SELINUX, "error", err)
		return err
	}
	return nil
}
