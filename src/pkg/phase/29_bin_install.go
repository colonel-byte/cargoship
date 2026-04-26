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

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/types/distrocfg"
	"github.com/k0sproject/rig/exec"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// BINUploadFiles implements a phase which upload files to hosts
type BINUploadFiles struct {
	UploadFilesCommon
	Distro distrocfg.Distro
}

// Title for the phase
func (p *BINUploadFiles) Title() string {
	return "Upload files to hosts -- Binaries"
}

func (p *BINUploadFiles) Explanation() string {
	return "Catch all phase if the combination of Operating System and Distro don't have other install methods"
}

// Prepare the phase
func (p *BINUploadFiles) Prepare(ctx context.Context, c *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	if err := p.UploadFilesCommon.Prepare(ctx, c, d); err != nil {
		logger.From(ctx).Warn("got", "error", err)
	}

	p.filesControl = p.getProfileFiles(ctx, config.SelectorBIN, cluster.RoleController)
	p.filesWorkers = p.getProfileFiles(ctx, config.SelectorBIN, cluster.RoleWorker)

	return nil
}

// Run the phase
func (p *BINUploadFiles) Run(ctx context.Context) (err error) {
	err = p.parallelDo(ctx, p.control, func(_ context.Context, zh *cluster.ZarfHost) error {
		zh.Metadata.Install = func(ctx context.Context, zh *cluster.ZarfHost) error {
			for _, f := range p.filesControl {
				logger.From(ctx).Debug("installing binary", "target", f.Target)
				if f.OriginalTarget != f.Target {
					err := zh.Exec(fmt.Sprintf("mv %s %s", f.Target, f.OriginalTarget), exec.Sudo(zh))
					if err != nil {
						logger.From(ctx).Warn("failed to move", "error", err)
					}
				}
			}
			return nil
		}
		return nil
	})
	if err != nil {
		logger.From(ctx).Warn("got", "error", err)
	}
	err = p.parallelDo(ctx, p.workers, func(_ context.Context, zh *cluster.ZarfHost) error {
		zh.Metadata.Install = func(ctx context.Context, zh *cluster.ZarfHost) error {
			for _, f := range p.filesWorkers {
				logger.From(ctx).Debug("installing binary", "target", f.Target)
				if f.OriginalTarget != f.Target {
					err := zh.Exec(fmt.Sprintf("mv %s %s", f.Target, f.OriginalTarget), exec.Sudo(zh))
					if err != nil {
						logger.From(ctx).Warn("failed to move", "error", err)
					}
				}
			}
			return nil
		}
		return nil
	})
	if err != nil {
		logger.From(ctx).Warn("got", "error", err)
	}

	return p.UploadFilesCommon.Run(ctx)
}
