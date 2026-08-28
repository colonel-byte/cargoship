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
	"errors"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/pkg/retry"
	configurer "github.com/colonel-byte/cargoship/src/types/os"
	rig "github.com/k0sproject/rig/v2"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// PrepareHosts installs required packages and so on on the hosts.
type PrepareHosts struct {
	GenericPhase
	d *distro.ZarfDistro
}

// Title for the phase
func (p *PrepareHosts) Title() string {
	return "Prepare hosts"
}

// Explanation about the current phase, used for documentation generation
func (p *PrepareHosts) Explanation() string {
	return "Updates the remote nodes; environment variables and sysctl"
}

// Prepare the phase
func (p *PrepareHosts) Prepare(_ context.Context, _ *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	p.d = d
	return nil
}

// Run the phase
func (p *PrepareHosts) Run(ctx context.Context) error {
	return p.parallelDo(ctx, p.manager.Config.Spec.Hosts, p.prepareHost)
}

type prepare interface {
	Prepare(configurer.Host) error
}

func (p *PrepareHosts) prepareHost(ctx context.Context, h *cluster.ZarfHost) error {
	if c, ok := h.Configurer.(prepare); ok {
		if err := c.Prepare(h); err != nil {
			return err
		}
	}

	if len(h.Environment) > 0 {
		logger.From(ctx).Info("updating environment", "host", h)
		if err := p.updateEnvironment(ctx, h, h.Environment); err != nil {
			return fmt.Errorf("failed to updated environment: %w", err)
		}
	}

	if len(p.d.Spec.Config.OS.Environment) > 0 {
		logger.From(ctx).Info("updating environment from the distro package", "host", h)
		if err := p.updateEnvironment(ctx, h, p.d.Spec.Config.OS.Environment); err != nil {
			return fmt.Errorf("failed to updated environment: %w", err)
		}
	}

	if len(p.manager.Distro.Spec.Config.OS.Sysctl) > 0 {
		logger.From(ctx).Info("updating sysctls", "host", h)
		if err := p.updateSysctlConfig(ctx, h); err != nil {
			return fmt.Errorf("failed to create sysctls config: %w", err)
		}
		if err := h.Sudo().Exec("sysctl --system"); err != nil {
			return fmt.Errorf("failed apply the new sysctls: %w", err)
		}
	}

	return nil
}

func (p *PrepareHosts) updateEnvironment(ctx context.Context, h *cluster.ZarfHost, env map[string]string) error {
	if err := h.Configurer.UpdateEnvironment(h, env); err != nil {
		return err
	}
	if h.ProtocolName() != "SSH" {
		return nil
	}

	// XXX: this is a workaround. UpdateEnvironment on rig's os/linux.go writes
	// the environment to /etc/environment and then exports the same variables
	// using 'export' command. This is not enough for the environment to be
	// preserved across multiple ssh sessions. We need to write the environment
	// and then reopen the ssh session. Go's ssh client.Setenv() depends on ssh
	// server configuration (sshd only accepts LC_* variables by default).
	logger.From(ctx).Info("reconnecting to apply new environment", "host", h)
	h.Disconnect()
	return retry.Timeout(ctx, 10*time.Minute, func(ctx context.Context) error {
		if err := h.Connect(ctx); err != nil {
			if errors.Is(err, rig.ErrNonRetryable) || strings.Contains(err.Error(), "host key mismatch") {
				return errors.Join(retry.ErrAbort, err)
			}
			return fmt.Errorf("failed to reconnect to %s: %w", h, err)
		}
		return nil
	})
}

func (p *PrepareHosts) updateSysctlConfig(ctx context.Context, h *cluster.ZarfHost) error {
	var sb strings.Builder

	keys := make([]string, 0, len(p.GetDistro().Spec.Config.OS.Sysctl))
	for k := range p.GetDistro().Spec.Config.OS.Sysctl {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	w := tabwriter.NewWriter(&sb, 1, 1, 1, ' ', 0)
	fmt.Fprintln(w, "# sysctl generated from cargoship")

	for _, key := range keys {
		fmt.Fprintf(w, "%s\t=\t%s\n", key, p.GetDistro().Spec.Config.OS.Sysctl[key])
	}
	if err := w.Flush(); err != nil {
		logger.From(ctx).Warn("failed to render sysctl tables")
	}

	return h.WriteFile("/etc/sysctl.d/99-cargoship.conf", sb.String(), "0644")
}
