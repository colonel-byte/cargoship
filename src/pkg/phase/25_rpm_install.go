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
	p.distro = p.manager.Distro.Spec.Config.OS.Files
	hosts := p.manager.Config.Spec.Hosts.Filter(utils.FilterEnterpriseLinux)

	p.workers = hosts.Filter(func(h *cluster.ZarfHost) bool {
		return !h.IsController()
	})

	p.control = hosts.Filter(func(h *cluster.ZarfHost) bool {
		return h.IsController()
	})

	p.filesControl = p.getProfileFiles(ctx, config.SelectorRPM, cluster.ROLE_CONTROLLER)
	p.filesWorkers = p.getProfileFiles(ctx, config.SelectorRPM, cluster.ROLE_WORKER)

	return nil
}

// ShouldRun is true when there are workers
func (p *RPMUploadFiles) ShouldRun() bool {
	return (len(p.control) + len(p.workers)) > 0
}
