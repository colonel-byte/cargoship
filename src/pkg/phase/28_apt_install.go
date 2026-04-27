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

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/pkg/utils"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// APTUploadFiles implements a phase which upload files to hosts
type APTUploadFiles struct {
	UploadFilesCommon
}

// Title for the phase
func (p *APTUploadFiles) Title() string {
	return "Upload files to hosts -- APT"
}

// Explanation about the current phase, used for documentation generation
func (p *APTUploadFiles) Explanation() string {
	return "If the remote node is a Debian based Operating System and the Distro package includes any files for those systems"
}

// Prepare the phase
func (p *APTUploadFiles) Prepare(ctx context.Context, c *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	if err := p.UploadFilesCommon.Prepare(ctx, c, d); err != nil {
		logger.From(ctx).Warn("got", "error", err)
	}

	p.control = p.control.Filter(utils.FilterEngineAlreadyPopulated).Filter(utils.FilterDebianLinux)
	p.workers = p.workers.Filter(utils.FilterEngineAlreadyPopulated).Filter(utils.FilterDebianLinux)

	p.filesControl = p.getProfileFiles(ctx, config.SelectorAPT, cluster.RoleController)
	p.filesWorkers = p.getProfileFiles(ctx, config.SelectorAPT, cluster.RoleWorker)

	return nil
}

// Run the phase
func (p *APTUploadFiles) Run(ctx context.Context) (err error) {
	err = p.parallelDo(ctx, p.control, func(_ context.Context, zh *cluster.ZarfHost) error {
		zh.Metadata.Install = func(_ context.Context, zh *cluster.ZarfHost) error {
			return zh.Configurer.InstallPackage(zh, getPath(p.filesControl)...)
		}
		return nil
	})
	if err != nil {
		logger.From(ctx).Warn("got", "error", err)
	}
	err = p.parallelDo(ctx, p.workers, func(_ context.Context, zh *cluster.ZarfHost) error {
		zh.Metadata.Install = func(_ context.Context, zh *cluster.ZarfHost) error {
			return zh.Configurer.InstallPackage(zh, getPath(p.filesWorkers)...)
		}
		return nil
	})
	if err != nil {
		logger.From(ctx).Warn("got", "error", err)
	}

	return p.UploadFilesCommon.Run(ctx)
}
