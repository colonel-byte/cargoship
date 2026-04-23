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
	"strings"

	v1alpha1 "github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// DetectOS performs remote OS detection
type DetectOS struct {
	GenericPhase
}

// Title for the phase
func (p *DetectOS) Title() string {
	return "Detect host operating systems"
}

// Run the phase
func (p *DetectOS) Run(ctx context.Context) error {
	return p.parallelDo(ctx, p.manager.Config.Spec.Hosts, func(_ context.Context, h *v1alpha1.ZarfHost) error {
		l := logger.From(ctx)

		if err := h.ResolveConfigurer(); err != nil {
			if h.OSVersion.IDLike != "" {
				l.Debug("trying to find a fallback OS support module", "host", h, "os version", h.OSVersion.String(), "like", h.OSVersion.IDLike)
				for id := range strings.SplitSeq(h.OSVersion.IDLike, " ") {
					h.OSVersion.ID = id
					if err := h.ResolveConfigurer(); err == nil {
						l.Warn("OS support fallback", "host", h, "id", id, "os version", h.OSVersion.String())
						return nil
					}
				}
			}
			return err
		}
		os := h.OSVersion.String()
		l.Info("running", "host", h, "os", os)

		return nil
	})
}
