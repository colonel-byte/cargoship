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
	"sort"
	"strings"
	"time"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/pkg/retry"
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/os"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// PrepareHosts installs required packages and so on on the hosts.
type PrepareHosts struct {
	GenericPhase
}

// Title for the phase
func (p *PrepareHosts) Title() string {
	return "Prepare hosts"
}

// Run the phase
func (p *PrepareHosts) Run(ctx context.Context) error {
	return p.parallelDo(ctx, p.manager.Config.Spec.Hosts, p.prepareHost)
}

type prepare interface {
	Prepare(os.Host) error
}

func (p *PrepareHosts) prepareHost(ctx context.Context, h *cluster.ZarfHost) error {
	if c, ok := h.Configurer.(prepare); ok {
		if err := c.Prepare(h); err != nil {
			return err
		}
	}

	if len(h.Environment) > 0 {
		logger.From(ctx).Info("updating environment", "host", h)
		if err := p.updateEnvironment(ctx, h); err != nil {
			return fmt.Errorf("failed to updated environment: %w", err)
		}
	}

	if len(p.manager.Distro.Spec.Config.OS.Sysctl) > 0 {
		logger.From(ctx).Info("updating sysctls", "host", h)
		if err := p.updateSysctls(ctx, h); err != nil {
			return fmt.Errorf("failed to updated sysctls: %w", err)
		}
	}

	return nil
}

func (p *PrepareHosts) updateEnvironment(ctx context.Context, h *cluster.ZarfHost) error {
	if err := h.Configurer.UpdateEnvironment(h, h.Environment); err != nil {
		return err
	}
	if h.Connection.Protocol() != "SSH" {
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
	return retry.Timeout(ctx, 10*time.Minute, func(_ context.Context) error {
		if err := h.Connect(); err != nil {
			if errors.Is(err, rig.ErrCantConnect) || strings.Contains(err.Error(), "host key mismatch") {
				return errors.Join(retry.ErrAbort, err)
			}
			return fmt.Errorf("failed to reconnect to %s: %w", h, err)
		}
		return nil
	})
}

func (p *PrepareHosts) updateSysctls(ctx context.Context, h *cluster.ZarfHost) error {
	keys := make([]string, 0, len(p.GetDistro().Spec.Config.OS.Sysctl))
	for k := range p.GetDistro().Spec.Config.OS.Sysctl {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for k := range keys {
		err := h.Configurer.SetSysctlValue(h, keys[k], p.GetDistro().Spec.Config.OS.Sysctl[keys[k]])
		if err != nil {
			logger.From(ctx).Warn("got error when setting sysctl value", "error", err)
		} else {
			logger.From(ctx).Debug("updating sysctls", "host", h, "key", keys[k])
		}
	}

	return nil
}
