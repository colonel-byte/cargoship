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
	"path/filepath"
	"slices"
	"strconv"

	"github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/zarf-distro/src/config"
	"github.com/colonel-byte/zarf-distro/src/types/os/linux/enterpriselinux"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// UploadFiles implements a phase which upload files to hosts
type RPMUploadFiles struct {
	GenericPhase

	workers cluster.ZarfHosts
	control cluster.ZarfHosts

	distro []distro.ZarfFile

	filesWorkers []cluster.UploadFile
	filesControl []cluster.UploadFile
}

// Title for the phase
func (p *RPMUploadFiles) Title() string {
	return "Upload files to hosts"
}

// Prepare the phase
func (p *RPMUploadFiles) Prepare(ctx context.Context, c *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	p.distro = p.manager.Distro.Spec.Config.OS.RPM
	hosts := p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		if len(p.distro) > 0 {
			switch h.Configurer.(type) {
			case *enterpriselinux.AlmaLinux, *enterpriselinux.AmazonLinux, *enterpriselinux.CentOS, *enterpriselinux.Fedora, *enterpriselinux.OracleLinux, *enterpriselinux.RHEL, *enterpriselinux.RockyLinux:
				return true
			default:
				return false
			}
		}
		return false
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
	err = p.parallelDoUpload(
		ctx,
		p.control,
		p.uploadRPMControllerFiles,
		p.installRPMControllerFiles,
	)
	if err != nil {
		return err
	}
	err = p.parallelDoUpload(
		ctx,
		p.workers,
		p.uploadRPMWorkerFiles,
		p.installRPMWorkerFiles,
	)
	if err != nil {
		return err
	}
	return nil
}

func (p *RPMUploadFiles) uploadRPMControllerFiles(ctx context.Context, h *cluster.ZarfHost) error {
	return p.uploadFiles(ctx, h, p.filesControl)
}

func (p *RPMUploadFiles) uploadRPMWorkerFiles(ctx context.Context, h *cluster.ZarfHost) error {
	return p.uploadFiles(ctx, h, p.filesWorkers)
}

func (p *RPMUploadFiles) installRPMControllerFiles(ctx context.Context, h *cluster.ZarfHost) error {
	return h.Configurer.InstallPackage(h, getPathOfRPM(p.filesControl)...)
}

func (p *RPMUploadFiles) installRPMWorkerFiles(ctx context.Context, h *cluster.ZarfHost) error {
	return h.Configurer.InstallPackage(h, getPathOfRPM(p.filesWorkers)...)
}

func getPathOfRPM(files []cluster.UploadFile) []string {
	rpm := []string{}

	for _, f := range files {
		rpm = append(rpm, f.DestinationFile)
	}

	return rpm
}

func (p *RPMUploadFiles) getProfileFiles(ctx context.Context, profile string) []cluster.UploadFile {
	files := []cluster.UploadFile{}

	for i, f := range p.distro {
		if f.Selector.Profiles != nil {
			if slices.Contains(f.Selector.Profiles, profile) {
				logger.From(ctx).Debug("determined this file needs to be uploaded", "file", filepath.Base(f.Target))
				files = append(files, cluster.UploadFile{
					Name:            filepath.Base(f.Target),
					DestinationFile: f.Target,
					Sources: []*cluster.LocalFile{
						{
							Path: filepath.Join(p.manager.TempDirectory, config.RPMDir, strconv.Itoa(i), filepath.Base(f.Target)),
						},
					},
				})
			}
		} else {
			logger.From(ctx).Debug("defaulting to upload", "file", filepath.Base(f.Target))
			files = append(files, cluster.UploadFile{
				Name:            filepath.Base(f.Target),
				DestinationFile: f.Target,
				Sources: []*cluster.LocalFile{
					{
						Path: filepath.Join(p.manager.TempDirectory, config.RPMDir, strconv.Itoa(i), filepath.Base(f.Target)),
					},
				},
			})
		}
	}

	return files
}
