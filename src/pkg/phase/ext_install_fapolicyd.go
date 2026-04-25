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
	// FAPOLICYD name of the service for fapolicyd
	FAPOLICYD = "fapolicyd"
)

// InstallFapolicy installs required packages and so on on the hosts.
type InstallFapolicy struct {
	GenericPhase
	Enabled        bool
	fapolicydhosts cluster.ZarfHosts
}

// Prepare the phase
func (p *InstallFapolicy) Prepare(ctx context.Context, _ *cluster.ZarfCluster, _ *distro.ZarfDistro) error {
	p.fapolicydhosts = p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return !h.Configurer.ServiceIsRunning(h, FAPOLICYD)
	})

	logger.From(ctx).Info("number of systems without fapolicy", "hosts", len(p.fapolicydhosts))

	return nil
}

// Title for the phase
func (p *InstallFapolicy) Title() string {
	return "Installing FAPolicyd"
}

// Run the phase
func (p *InstallFapolicy) Run(ctx context.Context) error {
	return p.parallelDo(ctx, p.fapolicydhosts, p.prepareHost)
}

// ShouldRun is true when there is a host with selinux or fapolicyd on the hosts
func (p *InstallFapolicy) ShouldRun() bool {
	return len(p.fapolicydhosts) > 0 && p.manager.Distro.Spec.Config.OS.FAPolicyd != "" && p.Enabled
}

func (p *InstallFapolicy) prepareHost(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("attempting to install", "host", h, "package", FAPOLICYD)
	err := h.Configurer.InstallPackage(h, FAPOLICYD)
	if err != nil {
		logger.From(ctx).Warn("could not install", "host", h, "package", FAPOLICYD, "error", err)
		return err
	}
	err = h.Configurer.EnableService(h, FAPOLICYD)
	if err != nil {
		logger.From(ctx).Warn("could not enable fapolicyd", "host", h, "error", err)
		return err
	}
	err = h.Configurer.StartService(h, FAPOLICYD)
	if err != nil {
		logger.From(ctx).Warn("could not start fapolicyd", "host", h, "error", err)
		return err
	}
	return nil
}
