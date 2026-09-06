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
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/config"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

func getPath(files []v1alpha1.ZarfFile) []string {
	filePath := []string{}

	for _, f := range files {
		filePath = append(filePath, f.Target)
	}

	return filePath
}

// hostFunc is one step of per-host work, as parallelDoUpload takes it.
type hostFunc = func(context.Context, *cluster.ZarfHost) error

// UploadFilesCommon implements a phase which upload files to hosts
type UploadFilesCommon struct {
	GenericPhase

	workers cluster.ZarfHosts
	control cluster.ZarfHosts

	distroFiles v1alpha1.ZarfFiles

	filesWorkers []v1alpha1.ZarfFile
	filesControl []v1alpha1.ZarfFile

	// priorManifest holds each host's upload manifest as it was before this run touched it,
	// captured in Prepare so Run can tell which of those files an upgrade no longer uploads.
	priorManifest map[*cluster.ZarfHost][]ManifestEntry
}

// Prepare the phase
func (p *UploadFilesCommon) Prepare(_ context.Context, _ *cluster.ZarfCluster, _ *distro.ZarfDistro) error {
	p.distroFiles = p.manager.Distro.Spec.Config.OS.Files
	hosts := p.manager.Config.Spec.Hosts

	p.workers = hosts.Filter(func(h *cluster.ZarfHost) bool {
		return !h.Metadata.EngineUploaded && !h.IsController()
	})

	p.control = hosts.Filter(func(h *cluster.ZarfHost) bool {
		return !h.Metadata.EngineUploaded && h.IsController()
	})

	p.priorManifest = make(map[*cluster.ZarfHost][]ManifestEntry, len(p.control)+len(p.workers))
	for _, h := range slices.Concat(p.control, p.workers) {
		p.priorManifest[h] = p.readManifest(h)
	}

	return nil
}

// Run the phase
func (p *UploadFilesCommon) Run(ctx context.Context) (err error) {
	err = p.parallelDoUpload(
		ctx,
		p.control,
		p.uploadSteps(p.uploadControllerFiles, p.filesControl)...,
	)
	if err != nil {
		return err
	}
	err = p.parallelDoUpload(
		ctx,
		p.workers,
		p.uploadSteps(p.uploadWorkerFiles, p.filesWorkers)...,
	)
	if err != nil {
		return err
	}

	return p.parallelDo(ctx, slices.Concat(p.control, p.workers), p.cleanStaleUploads)
}

// uploadSteps is the per-host work for one role: the upload itself, and the mark that keeps the
// later install phases off the host. The mark is only added when this phase has files for the
// role. A phase whose package carries nothing for it -- an APT phase against a package with no
// deb files, say -- must leave the host unmarked, so the binary phase, the catch-all that exists
// for that case, still claims it. Marking a host the phase uploaded nothing to leaves it with no
// engine, and the first sign of that is a controller phase waiting out its retry budget on a
// service that was never installed.
func (p *UploadFilesCommon) uploadSteps(upload hostFunc, files []v1alpha1.ZarfFile) []hostFunc {
	if len(files) == 0 {
		return []hostFunc{upload}
	}

	return []hostFunc{upload, p.blockOtherInstalls}
}

// cleanStaleUploads removes engine files this run's upload left in the manifest from a previous
// version but didn't re-upload itself, e.g. an engine binary a version bump renamed. The diff is
// scoped to the "engine" category: other upload phases (e.g. images) record into the same
// per-host UploadedFiles list, and their entries may not exist yet by the time this phase runs.
func (p *UploadFilesCommon) cleanStaleUploads(ctx context.Context, h *cluster.ZarfHost) error {
	current := parseManifest(strings.Join(h.Metadata.UploadedFiles, "\n"))
	old := filterManifestByCategory(p.priorManifest[h], "engine")
	current = filterManifestByCategory(current, "engine")
	p.removeStaleManifestEntries(ctx, h, old, current)
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

// ShouldRun is true when there are workers
func (p *UploadFilesCommon) ShouldRun() bool {
	return (len(p.control) + len(p.workers)) > 0
}

func (p *UploadFilesCommon) getProfileFiles(ctx context.Context, selector string, profile string) []v1alpha1.ZarfFile {
	files := []v1alpha1.ZarfFile{}

	for i, f := range p.distroFiles {
		switch f.Selector.Package {
		case selector:
			if f.Selector.Profile == "" || f.Selector.Profile == profile {
				logger.From(ctx).Debug("determined this file needs to be uploaded", "file", filepath.Base(f.Target))
				filePath := filepath.Join(p.manager.TempDirectory, config.OSDir, strconv.Itoa(i), filepath.Base(f.Target))
				err := os.Chtimes(filePath, time.Unix(0, 0), time.Unix(0, 0))
				if err != nil {
					logger.From(ctx).Warn("failed to change the file time", "error", err)
				}
				target := f.Target
				if f.Executable {
					target = stageTempPath(false, f.Target)
				}
				files = append(files, v1alpha1.ZarfFile{
					Name:           filepath.Base(f.Target),
					Target:         target,
					OriginalTarget: f.Target,
					Category:       "engine",
					LocalSource: v1alpha1.LocalFile{
						Path: filePath,
					},
				})
			}
		default:
			logger.From(ctx).Debug("not selected for upload", "file", filepath.Base(f.Target))
		}
	}

	return files
}

// CleanUp the phase
func (p *UploadFilesCommon) CleanUp(ctx context.Context) {
	err := p.parallelDo(context.Background(), p.manager.Config.Spec.Hosts, func(_ context.Context, h *cluster.ZarfHost) error {
		if len(h.Metadata.BinaryTempFile) == 0 {
			return nil
		}
		logger.From(ctx).Info("cleaning up binary tempfile", "host", h)
		for _, f := range h.Metadata.BinaryTempFile {
			logger.From(ctx).Debug("removing file", "file", f, "host", h)
			if err := h.Configurer.DeleteFile(h, f); err != nil {
				logger.From(ctx).Warn("failed to delete", "host", h, "file", f, "error", err)
			}
		}
		return nil
	})
	if err != nil {
		logger.From(ctx).Warn("failed to clean up tempfiles")
	}
}
