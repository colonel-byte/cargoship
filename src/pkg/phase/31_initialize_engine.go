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

	"github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/zarf-distro/src/types/distro"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type InitializeEngine struct {
	GenericPhase
	Distro distro.Distro
	hosts  cluster.ZarfHosts
}

// Title returns the phase title
func (p *InitializeEngine) Title() string {
	return "Initialize engine"
}

func (p *InitializeEngine) Run(ctx context.Context) error {
	return nil
}

func (p *InitializeEngine) CleanUp(ctx context.Context) {
	err := p.parallelDo(context.Background(), p.hosts, func(_ context.Context, h *cluster.ZarfHost) error {
		if h.Metadata.BinaryTempFile == "" {
			return nil
		}
		logger.From(ctx).Info("cleaning up k0s binary tempfile", "host", h)
		_ = h.Configurer.DeleteFile(h, h.Metadata.BinaryTempFile)
		return nil
	})
	if err != nil {
		logger.From(ctx).Warn("failed to clean up tempfiles")
	}
}
