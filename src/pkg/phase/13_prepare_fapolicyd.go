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
	v1alpha1 "github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

const (
	FAPOLICYD_RULES_FILE = "/etc/fapolicyd/rules.d/31-cargoship.rules"
)

// PrepareHosts installs required packages and so on on the hosts.
type PrepareFapolicy struct {
	GenericPhase
	fapolicydhosts cluster.ZarfHosts
}

// Prepare the phase
func (p *PrepareFapolicy) Prepare(ctx context.Context, c *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	p.fapolicydhosts = p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return h.Configurer.ServiceIsRunning(h, FAPOLICYD)
	})

	logger.From(ctx).Info("number of systems with fapolicy", "hosts", len(p.fapolicydhosts))

	return nil
}

// Title for the phase
func (p *PrepareFapolicy) Title() string {
	return "Prepare hosts - Enterprise Linux support - Fapolicyd"
}

// Run the phase
func (p *PrepareFapolicy) Run(ctx context.Context) error {
	return p.parallelDo(ctx, p.fapolicydhosts, p.prepareHost)
}

// ShouldRun is true when there is a host with selinux or fapolicyd on the hosts
func (p *PrepareFapolicy) ShouldRun() bool {
	return len(p.fapolicydhosts) > 0 && p.manager.Distro.Spec.Config.OS.FAPolicyd != ""
}

func (p *PrepareFapolicy) prepareHost(ctx context.Context, h *v1alpha1.ZarfHost) error {
	err := h.Configurer.WriteFile(h, FAPOLICYD_RULES_FILE, p.manager.Distro.Spec.Config.OS.FAPolicyd, "0644")
	if err != nil {
		return err
	}
	logger.From(ctx).Info("wrote fapolicyd file, restarting serivce", "host", h)
	return h.Configurer.RestartService(h, FAPOLICYD)
}
