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
	"slices"

	"github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/zarf-distro/src/types/os/linux/enterpriselinux"
)

// UploadFiles implements a phase which upload files to hosts
type RPMUploadFiles struct {
	GenericPhase

	workers cluster.ZarfHosts
	control cluster.ZarfHosts

	filesWorkers []distro.ZarfFile
	filesControl []distro.ZarfFile
}

// Title for the phase
func (p *RPMUploadFiles) Title() string {
	return "Upload files to hosts"
}

// Prepare the phase
func (p *RPMUploadFiles) Prepare(ctx context.Context, c *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	hosts := p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		switch h.Configurer.(type) {
		case *enterpriselinux.AlmaLinux, *enterpriselinux.AmazonLinux, *enterpriselinux.CentOS, *enterpriselinux.Fedora, *enterpriselinux.OracleLinux, *enterpriselinux.RHEL, *enterpriselinux.RockyLinux:
			return true
		default:
			return false
		}
	})

	p.workers = hosts.Filter(func(h *cluster.ZarfHost) bool {
		return !h.IsController()
	})

	p.control = hosts.Filter(func(h *cluster.ZarfHost) bool {
		return h.IsController()
	})

	p.filesControl = p.getProfileFiles(ctx, cluster.ROLE_CONTROLLER)
	p.filesWorkers = p.getProfileFiles(ctx, cluster.ROLE_WORKER)

	return nil
}

// ShouldRun is true when there are workers
func (p *RPMUploadFiles) ShouldRun() bool {
	return (len(p.control) + len(p.workers)) > 0
}

// Run the phase
func (p *RPMUploadFiles) Run(ctx context.Context) (err error) {
	err = p.parallelDoUpload(ctx, p.control, p.uploadRPMControllerFiles)
	if err != nil {
		return err
	}
	err = p.parallelDoUpload(ctx, p.workers, p.uploadRPMWorkerFiles)
	if err != nil {
		return err
	}
	return nil
}

func (p *RPMUploadFiles) getProfileFiles(ctx context.Context, profile string) []distro.ZarfFile {
	files := []distro.ZarfFile{}

	for _, f := range p.manager.Distro.Spec.Config.OS.RPM {
		if f.Selector.Profiles != nil {
			if slices.Contains(f.Selector.Profiles, profile) {
				files = append(files, f)
			}
		} else {
			files = append(files, f)
		}
	}

	return files
}

func (p *RPMUploadFiles) uploadRPMControllerFiles(ctx context.Context, h *cluster.ZarfHost) error {

	return nil
}

func (p *RPMUploadFiles) uploadRPMWorkerFiles(ctx context.Context, h *cluster.ZarfHost) error {

	return nil
}
