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
	"strings"
	"time"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/pkg/retry"
	rig "github.com/k0sproject/rig/v2"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// ErrConnect marks a failure to reach or authenticate to a host. It is joined into whatever the
// SSH layer returned so that a caller -- Execute, deciding a process exit code -- can tell "the
// fleet was unreachable" from "a phase did its work and failed", without importing rig.
var ErrConnect = errors.New("cannot connect to host")

// Connect connects to each of the hosts
type Connect struct {
	GenericPhase
}

// Title for the phase
func (p *Connect) Title() string {
	return "Connect to hosts"
}

// Explanation about the current phase, used for documentation generation
func (p *Connect) Explanation() string {
	return "Connects to a remote host via `github.com/k0sproject/rig`"
}

// ReadOnly marks this phase safe under a dry run, and returns the reason for the phase docs.
func (p *Connect) ReadOnly() string {
	return "Connects opens the SSH session to each host and does nothing else. A dry run needs it, because a preflight that never reached a host would report on a cluster it never looked at."
}

// Run the phase
func (p *Connect) Run(ctx context.Context) error {
	return p.parallelDo(ctx, p.manager.Config.Spec.Hosts, func(ctx context.Context, h *cluster.Host) error {
		return retry.Timeout(ctx, 10*time.Minute, func(ctx context.Context) error {
			if err := h.Connect(ctx); err != nil {
				logger.From(ctx).Debug("got the following", "host", h, "err", err)
				if errors.Is(err, rig.ErrNonRetryable) || strings.Contains(err.Error(), "host key mismatch") {
					return errors.Join(retry.ErrAbort, ErrConnect, err)
				}
				// Rejected credentials abort too. rig deliberately leaves ErrAuthFailed
				// retryable, because a caller that provisions a host may connect while sshd is
				// up but authorized_keys is not yet written -- and its documentation says a
				// caller that would rather fail fast should test for it and stop its own loop.
				//
				// Cargoship is that caller: it manages hosts that already exist and never
				// creates one, so the window rig protects does not occur here. What does occur
				// is a wrong user or a wrong identity file in the inventory, which no amount of
				// waiting fixes -- and retrying it burned the full ten minutes before telling
				// the operator about a typo.
				if errors.Is(err, rig.ErrAuthFailed) {
					return errors.Join(retry.ErrAbort, ErrConnect, err)
				}
				return errors.Join(ErrConnect, err)
			}
			logger.From(ctx).Info("connected", "host", h)
			return nil
		})
	})
}
