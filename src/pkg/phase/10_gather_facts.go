// Copyright 2023 k0sctl authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from k0sctl:
// https://github.com/k0sproject/k0sctl
//
// Modifications Copyright 2026 colonel-byte.
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

	"github.com/colonel-byte/cargoship/src/api"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// GatherFacts gathers information about hosts
type GatherFacts struct {
	GenericPhase
	profiles map[string]cluster.ZarfClusterProfiles
}

// Title for the phase
func (p *GatherFacts) Title() string {
	return "Gather host facts"
}

// Explanation about the current phase, used for documentation generation
func (p *GatherFacts) Explanation() string {
	return "Gathers network related information about the remote host, including: Hostname, Private Address, Private Interface. Will also update the hosts based off the profile if configured in the config file."
}

// Run the phase
func (p *GatherFacts) Run(ctx context.Context) error {
	p.profiles = p.manager.Config.Spec.Config.Profiles

	return p.parallelDo(
		ctx,
		p.manager.Config.Spec.Hosts,
		p.investigateHost,
		p.setupProfileOverrides,
	)
}

// investigateHost gathers network and host-specific facts, such as architecture, hostname, private interface, and private address, for a given host.
//
// ctx: Context for logging and cancellation.
// h: Pointer to the cluster.ZarfHost to investigate.
// Returns an error if resolution fails.
func (p *GatherFacts) investigateHost(ctx context.Context, h *cluster.ZarfHost) error {
	l := logger.From(ctx)

	arch, err := h.Arch()
	if err != nil {
		return err
	}

	l.Info("detected", "host", h, "arch", arch)

	if h.Hostname != "" {
		l.Info("using provided hostname", "host", h, "given", h.Hostname)
		h.Metadata.Hostname = h.Hostname
	} else {
		n := h.Configurer.Hostname(h)
		if n == "" {
			return fmt.Errorf("%s: failed to resolve a hostname", h)
		}
		h.Metadata.Hostname = n
		l.Info("using discovered hostname", "host", h, "found", n)
	}

	if h.PrivateAddress == "" {
		if h.PrivateInterface == "" {
			if iface, err := h.Configurer.PrivateInterface(h); err == nil {
				h.PrivateInterface = iface
				l.Info("discoverd", "host", h, "interface", iface)
			}
		}

		if h.PrivateInterface != "" {
			if addr, err := h.Configurer.PrivateAddress(h, h.PrivateInterface, h.Address()); err == nil {
				h.PrivateAddress = addr
				l.Info("discoverd", "host", h, "address", addr)
			}
		}
	}

	return nil
}

// setupProfileOverrides merges profile-specific engine and host configurations into the given host if a matching profile is found.
//
// ctx: Context for logging.
// h: Pointer to the cluster.ZarfHost to apply profile overrides to.
// Returns an error if any occurs during the setup.
func (p *GatherFacts) setupProfileOverrides(ctx context.Context, h *cluster.ZarfHost) error {
	if profile, ok := p.profiles[h.Profile]; ok {
		h.Engine.Merge(profile.Engine)
		h.Host.Merge(profile.Host)
	}

	logger.From(ctx).Info("testing", "host", h, "profile", h.Profile)

	return nil
}

// hostArch returns a host's CPU architecture as a typed api.Arch.
//
// h.Arch caches the value the configurer detected, which Linux.Arch has already mapped onto the
// names cargoship uses, so this is a lookup rather than another round trip once investigateHost has
// run. ParseArch then rejects an architecture cargoship cannot target, such as the arm reported for
// a 32-bit host.
func hostArch(h *cluster.ZarfHost) (api.Arch, error) {
	raw, err := h.Arch()
	if err != nil {
		return "", err
	}

	arch, err := api.ParseArch(raw)
	if err != nil {
		return "", fmt.Errorf("%s: %w", h, err)
	}

	return arch, nil
}
