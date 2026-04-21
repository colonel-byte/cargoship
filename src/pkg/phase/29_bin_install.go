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

	"github.com/colonel-byte/mare/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/mare/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/mare/src/config"
)

// UploadFiles implements a phase which upload files to hosts
type BINUploadFiles struct {
	UploadFilesCommon
}

// Title for the phase
func (p *BINUploadFiles) Title() string {
	return "Upload files to hosts -- Binaries"
}

// Prepare the phase
func (p *BINUploadFiles) Prepare(ctx context.Context, c *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	p.UploadFilesCommon.Prepare(ctx, c, d)

	p.filesControl = p.getProfileFiles(ctx, config.SelectorBIN, cluster.ROLE_CONTROLLER)
	p.filesWorkers = p.getProfileFiles(ctx, config.SelectorBIN, cluster.ROLE_WORKER)

	return nil
}
