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
	"strconv"

	"github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/zarf-distro/src/config"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

func getPath(files []cluster.UploadFile) []string {
	file_path := []string{}

	for _, f := range files {
		file_path = append(file_path, f.DestinationFile)
	}

	return file_path
}

// UploadFiles implements a phase which upload files to hosts
type UploadFilesCommon struct {
	GenericPhase

	workers cluster.ZarfHosts
	control cluster.ZarfHosts

	distro distro.ZarfFiles

	filesWorkers []cluster.UploadFile
	filesControl []cluster.UploadFile
}

func (p *UploadFilesCommon) Prepare(ctx context.Context, c *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	p.distro = p.manager.Distro.Spec.Config.OS.Files
	hosts := p.manager.Config.Spec.Hosts

	p.workers = hosts.Filter(func(h *cluster.ZarfHost) bool {
		return !h.Metadata.EngineUploaded && !h.IsController()
	})

	p.control = hosts.Filter(func(h *cluster.ZarfHost) bool {
		return !h.Metadata.EngineUploaded && h.IsController()
	})

	return nil
}

// Run the phase
func (p *UploadFilesCommon) Run(ctx context.Context) (err error) {
	err = p.parallelDoUpload(
		ctx,
		p.control,
		p.uploadControllerFiles,
		p.installControllerFiles,
		p.blockOtherInstalls,
	)
	if err != nil {
		return err
	}
	err = p.parallelDoUpload(
		ctx,
		p.workers,
		p.uploadWorkerFiles,
		p.installWorkerFiles,
		p.blockOtherInstalls,
	)
	if err != nil {
		return err
	}
	return nil
}

func (p *UploadFilesCommon) blockOtherInstalls(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Debug("disabling host from other installs", "host", h)
	h.Metadata.EngineUploaded = true
	return nil
}

func (p *UploadFilesCommon) uploadControllerFiles(ctx context.Context, h *cluster.ZarfHost) error {
	return p.uploadFiles(ctx, h, p.filesControl)
}

func (p *UploadFilesCommon) uploadWorkerFiles(ctx context.Context, h *cluster.ZarfHost) error {
	return p.uploadFiles(ctx, h, p.filesWorkers)
}

func (p *UploadFilesCommon) installControllerFiles(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("installing engine - controller", "host", h)
	return h.Configurer.InstallPackage(h, getPath(p.filesControl)...)
}

func (p *UploadFilesCommon) installWorkerFiles(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("installing engine - worker", "host", h)
	return h.Configurer.InstallPackage(h, getPath(p.filesWorkers)...)
}

// ShouldRun is true when there are workers
func (p *UploadFilesCommon) ShouldRun() bool {
	return (len(p.control) + len(p.workers)) > 0
}

func (p *UploadFilesCommon) getProfileFiles(ctx context.Context, selector string, profile string) []cluster.UploadFile {
	files := []cluster.UploadFile{}

	for i, f := range p.distro {
		switch f.Selector.Package {
		case selector:
			if f.Selector.Profile == "" || f.Selector.Profile == profile {
				logger.From(ctx).Debug("determined this file needs to be uploaded", "file", filepath.Base(f.Target))
				files = append(files, cluster.UploadFile{
					Name:            filepath.Base(f.Target),
					DestinationFile: f.Target,
					Sources: []*cluster.LocalFile{
						{
							Path: filepath.Join(p.manager.TempDirectory, config.OSDir, strconv.Itoa(i), filepath.Base(f.Target)),
						},
					},
				})
			}
		default:
			logger.From(ctx).Debug("not selected for upload", "file", filepath.Base(f.Target))
		}
	}

	return files
}

func (p *UploadFilesCommon) CleanUp(ctx context.Context) {
	err := p.parallelDo(context.Background(), p.manager.Config.Spec.Hosts, func(_ context.Context, h *cluster.ZarfHost) error {
		if len(h.Metadata.BinaryTempFile) == 0 {
			return nil
		}
		logger.From(ctx).Info("cleaning up binary tempfile", "host", h)
		for _, f := range h.Metadata.BinaryTempFile {
			logger.From(ctx).Debug("removing file", "file", f, "host", h)
			_ = h.Configurer.DeleteFile(h, f)
		}
		return nil
	})
	if err != nil {
		logger.From(ctx).Warn("failed to clean up tempfiles")
	}
}
