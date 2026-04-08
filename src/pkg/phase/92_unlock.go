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
	"github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/distro"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type Unlock struct {
	GenericPhase
	Cancel func(context.Context)
}

// Prepare the phase
func (p *Unlock) Prepare(_ context.Context, c *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	p.manager.Config = c
	if p.Cancel == nil {
		p.Cancel = func(ctx context.Context) {
			logger.From(ctx).Warn("cancel function not defined")
		}
	}
	return nil
}

// Title for the phase
func (p *Unlock) Title() string {
	return "Release exclusive host lock"
}

// Run the phase
func (p *Unlock) Run(ctx context.Context) error {
	p.Cancel(ctx)
	return nil
}
