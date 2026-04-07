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

	v1alpha1 "github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// GatherFacts gathers information about hosts, such as if k0s is already up and running
type GatherFacts struct {
	GenericPhase
}

// Title for the phase
func (p *GatherFacts) Title() string {
	return "Gather host facts"
}

// Run the phase
func (p *GatherFacts) Run(ctx context.Context) error {
	return p.parallelDo(ctx, p.manager.Config.Spec.Hosts, p.investigateHost)
}

func (p *GatherFacts) investigateHost(ctx context.Context, h *v1alpha1.ZarfHost) error {
	l := logger.From(ctx)

	arch, err := h.Arch()
	if err != nil {
		return err
	}

	l.Info("detected", "host", h, "arch", arch)

	id, err := h.Configurer.MachineID(h)
	if err != nil {
		return err
	}
	h.Metadata.MachineID = id

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
