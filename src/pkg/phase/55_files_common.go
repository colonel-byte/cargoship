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

	"github.com/colonel-byte/cargoship/src/api"
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

// UploadFilesCommon implements a phase which upload files to hosts
type UploadFilesCommon struct {
	GenericPhase

	workers cluster.ZarfHosts
	control cluster.ZarfHosts

	distroFiles v1alpha1.ZarfFiles

	// filesWorkers and filesControl hold the files to upload, keyed by the architecture of the
	// host receiving them. A package targeting one architecture has a single entry; a host looks
	// its own up rather than taking the whole list.
	filesWorkers map[api.Arch][]v1alpha1.ZarfFile
	filesControl map[api.Arch][]v1alpha1.ZarfFile

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
		p.uploadControllerFiles,
		p.blockOtherInstalls,
	)
	if err != nil {
		return err
	}
	err = p.parallelDoUpload(
		ctx,
		p.workers,
		p.uploadWorkerFiles,
		p.blockOtherInstalls,
	)
	if err != nil {
		return err
	}

	return p.parallelDo(ctx, slices.Concat(p.control, p.workers), p.cleanStaleUploads)
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
	return p.uploadFiles(ctx, h, p.filesFor(ctx, p.filesControl, h))
}

func (p *UploadFilesCommon) uploadWorkerFiles(ctx context.Context, h *cluster.ZarfHost) error {
	return p.uploadFiles(ctx, h, p.filesFor(ctx, p.filesWorkers, h))
}

// filesFor picks the files built for a host's own architecture.
//
// An architecture that cannot be resolved yields no files rather than an error. ValidateHosts has
// already failed the run for a host the package does not carry, so reaching this at upload time
// means the host never reported an architecture at all, and uploading another architecture's
// binaries to it would be worse than uploading nothing.
func (p *UploadFilesCommon) filesFor(ctx context.Context, byArch map[api.Arch][]v1alpha1.ZarfFile, h *cluster.ZarfHost) []v1alpha1.ZarfFile {
	arch, err := hostArch(h)
	if err != nil {
		logger.From(ctx).Warn("could not determine the host architecture, uploading no files", "host", h, "error", err)
		return nil
	}

	return byArch[arch]
}

// ShouldRun is true when there are workers
func (p *UploadFilesCommon) ShouldRun() bool {
	return (len(p.control) + len(p.workers)) > 0
}

// getProfileFiles groups the files this phase uploads by the architecture of the host receiving
// them, so a cluster of mixed CPUs gets the right binaries on each node.
func (p *UploadFilesCommon) getProfileFiles(ctx context.Context, selector string, profile string) map[api.Arch][]v1alpha1.ZarfFile {
	// p.control and p.workers are read as they stand, so a phase that narrows them first, as the
	// RPM and APT phases do, only builds file sets for architectures it will actually upload.
	arches := hostArches(ctx, slices.Concat(p.control, p.workers))
	byArch := make(map[api.Arch][]v1alpha1.ZarfFile, len(arches))

	for _, arch := range arches {
		byArch[arch] = p.profileFilesForArch(ctx, selector, profile, arch)
	}

	return byArch
}

// profileFilesForArch is the file list for one architecture.
//
// A file's position in p.distroFiles is the name of the directory it was assembled into, so a file
// that does not apply is skipped inside the loop rather than filtered out of the list beforehand.
// Filtering first would renumber every later file and read the wrong bytes off disk.
func (p *UploadFilesCommon) profileFilesForArch(ctx context.Context, selector string, profile string, arch api.Arch) []v1alpha1.ZarfFile {
	files := []v1alpha1.ZarfFile{}

	for i, f := range p.distroFiles {
		if f.Selector.Package != selector {
			logger.From(ctx).Debug("not selected for upload", "file", filepath.Base(f.Target))
			continue
		}
		if f.Selector.Profile != "" && f.Selector.Profile != profile {
			continue
		}
		if !f.Selector.MatchesArch(arch) {
			logger.From(ctx).Debug("file is not for this architecture", "file", filepath.Base(f.Target), "arch", arch)
			continue
		}

		logger.From(ctx).Debug("determined this file needs to be uploaded", "file", filepath.Base(f.Target), "arch", arch)
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
