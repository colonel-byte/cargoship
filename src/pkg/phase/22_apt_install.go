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
	"github.com/colonel-byte/zarf-distro/src/types/os/linux"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// UploadFiles implements a phase which upload files to hosts
type APTUploadFiles struct {
	GenericPhase

	workers cluster.ZarfHosts
	control cluster.ZarfHosts

	distro []distro.ZarfFile

	filesWorkers []cluster.UploadFile
	filesControl []cluster.UploadFile
}

// Title for the phase
func (p *APTUploadFiles) Title() string {
	return "Upload files to hosts"
}

// Prepare the phase
func (p *APTUploadFiles) Prepare(ctx context.Context, c *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	p.distro = p.manager.Distro.Spec.Config.OS.APT
	hosts := p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		if len(p.distro) > 0 {
			switch h.Configurer.(type) {
			case *linux.Debian, *linux.Ubuntu:
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
func (p *APTUploadFiles) ShouldRun() bool {
	return (len(p.control) + len(p.workers)) > 0
}

// Run the phase
func (p *APTUploadFiles) Run(ctx context.Context) (err error) {
	err = p.parallelDoUpload(ctx, p.control, p.uploadAPTControllerFiles, p.installAPTControllerFiles)
	if err != nil {
		return err
	}
	err = p.parallelDoUpload(ctx, p.workers, p.uploadAPTWorkerFiles, p.installAPTWorkerFiles)
	if err != nil {
		return err
	}
	return nil
}

func (p *APTUploadFiles) uploadAPTControllerFiles(ctx context.Context, h *cluster.ZarfHost) error {
	return p.uploadFiles(ctx, h, p.filesControl)
}

func (p *APTUploadFiles) uploadAPTWorkerFiles(ctx context.Context, h *cluster.ZarfHost) error {
	return p.uploadFiles(ctx, h, p.filesWorkers)
}

func (p *APTUploadFiles) installAPTControllerFiles(ctx context.Context, h *cluster.ZarfHost) error {
	return h.Configurer.InstallPackage(h, getPathOfAPT(p.filesControl)...)
}

func (p *APTUploadFiles) installAPTWorkerFiles(ctx context.Context, h *cluster.ZarfHost) error {
	return h.Configurer.InstallPackage(h, getPathOfAPT(p.filesWorkers)...)
}

func getPathOfAPT(files []cluster.UploadFile) []string {
	apt := []string{}

	for _, f := range files {
		apt = append(apt, f.DestinationFile)
	}

	return apt
}

func (p *APTUploadFiles) getProfileFiles(ctx context.Context, profile string) []cluster.UploadFile {
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
