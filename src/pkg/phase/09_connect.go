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
	"strings"
	"time"

	v1alpha1 "github.com/colonel-byte/mare/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/mare/src/pkg/retry"
	"github.com/k0sproject/rig"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// Connect connects to each of the hosts
type Connect struct {
	GenericPhase
}

// Title for the phase
func (p *Connect) Title() string {
	return "Connect to hosts"
}

func (p *Connect) Run(ctx context.Context) error {
	return p.parallelDo(ctx, p.manager.Config.Spec.Hosts, func(ctx context.Context, h *v1alpha1.ZarfHost) error {
		return retry.Timeout(ctx, 10*time.Minute, func(_ context.Context) error {
			if err := h.Connect(); err != nil {
				logger.From(ctx).Debug("got the following", "host", h, "err", err)
				if errors.Is(err, rig.ErrCantConnect) || strings.Contains(err.Error(), "host key mismatch") {
					return errors.Join(retry.ErrAbort, err)
				}
				return err
			}

			logger.From(ctx).Info("connected", "host", h)

			return nil
		})
	})
}
