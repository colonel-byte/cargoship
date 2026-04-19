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
	"github.com/colonel-byte/zarf-distro/src/config"
	"github.com/colonel-byte/zarf-distro/src/pkg/utils"
)

// UploadFiles implements a phase which upload files to hosts
type RPMUploadFiles struct {
	UploadFilesCommon
}

// Title for the phase
func (p *RPMUploadFiles) Title() string {
	return "Upload files to hosts -- RPM"
}

// Prepare the phase
func (p *RPMUploadFiles) Prepare(ctx context.Context, c *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	p.UploadFilesCommon.Prepare(ctx, c, d)

	p.control = p.control.Filter(utils.FilterEnterpriseLinux)
	p.workers = p.workers.Filter(utils.FilterEnterpriseLinux)

	p.filesControl = p.getProfileFiles(ctx, config.SelectorRPM, cluster.ROLE_CONTROLLER)
	p.filesWorkers = p.getProfileFiles(ctx, config.SelectorRPM, cluster.ROLE_WORKER)

	return nil
}

// Run the phase
func (p *RPMUploadFiles) Run(ctx context.Context) (err error) {
	p.parallelDo(ctx, p.control, func(ctx context.Context, zh *cluster.ZarfHost) error {
		zh.Metadata.Install = func(ctx context.Context, zh *cluster.ZarfHost) error {
			return zh.Configurer.InstallPackage(zh, getPath(p.filesControl)...)
		}
		return nil
	})
	p.parallelDo(ctx, p.workers, func(ctx context.Context, zh *cluster.ZarfHost) error {
		zh.Metadata.Install = func(ctx context.Context, zh *cluster.ZarfHost) error {
			return zh.Configurer.InstallPackage(zh, getPath(p.filesWorkers)...)
		}
		return nil
	})

	return p.UploadFilesCommon.Run(ctx)
}
