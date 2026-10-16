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
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/colonel-byte/cargoship/api"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/helmvalues"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// whenEnabled reports whether a file's Selector.When gate is satisfied by the package's values.
// An empty path is no gate at all and always passes, as does a path holding true.
//
// A path absent from values reads as disabled, which is also exactly what a typo in a distro's
// when path looks like. The symptom either way is a file that never arrives, so an absent path is
// logged at warn rather than debug - a path that is present and false is the normal way to leave
// an optional file switched off, and stays quiet.
func whenEnabled(ctx context.Context, values map[string]any, path string, name string) bool {
	if path == "" {
		return true
	}

	v, ok, err := helmvalues.GetValuePath(values, path)
	switch {
	case err != nil:
		logger.From(ctx).Warn("file skipped: malformed when path", "file", name, "when", path, "error", err)
		return false
	case !ok:
		logger.From(ctx).Warn("file skipped: when path is not set in the package's values", "file", name, "when", path)
		return false
	}

	enabled, err := truthy(v)
	if err != nil {
		logger.From(ctx).Warn("file skipped: when path is not a boolean", "file", name, "when", path, "error", err)
		return false
	}
	if !enabled {
		logger.From(ctx).Debug("file skipped: when path is false", "file", name, "when", path)
	}

	return enabled
}

// truthy reads a when path's value as a boolean. The schema types these as booleans, but values
// reach here from a YAML decoder or from a --set on the command line, so a string spelling of one
// is accepted too.
func truthy(v any) (bool, error) {
	switch val := v.(type) {
	case bool:
		return val, nil
	case string:
		return strconv.ParseBool(val)
	default:
		return false, fmt.Errorf("want a boolean, got %T", v)
	}
}

// hostProfile is the Selector.Profile value a host matches. The selector names a role rather
// than a host's own profile, which may be anything a cluster config gives it -- an infra node
// joins as a worker and takes worker files.
func hostProfile(h *cluster.Host) string {
	if h.IsController() {
		return cluster.RoleController
	}
	return cluster.RoleWorker
}

func getPath(files []v1alpha1.File) []string {
	filePath := []string{}

	for _, f := range files {
		filePath = append(filePath, f.Target)
	}

	return filePath
}

// UploadFilesCommon implements a phase which upload files to hosts
type UploadFilesCommon struct {
	GenericPhase

	workers cluster.Hosts
	control cluster.Hosts

	distroFiles v1alpha1.Files

	// filesWorkers and filesControl hold the files to upload, keyed by the architecture of the
	// host receiving them. A package targeting one architecture has a single entry; a host looks
	// its own up rather than taking the whole list.
	filesWorkers map[api.Arch][]v1alpha1.File
	filesControl map[api.Arch][]v1alpha1.File

	// priorManifest holds each host's upload manifest as it was before this run touched it,
	// captured in Prepare so Run can tell which of those files an upgrade no longer uploads.
	priorManifest map[*cluster.Host][]ManifestEntry
}

// Prepare the phase
func (p *UploadFilesCommon) Prepare(_ context.Context, _ *cluster.Cluster, _ *distro.Distro) error {
	p.distroFiles = p.manager.Distro.Spec.Config.OS.Files
	hosts := p.manager.Config.Spec.Hosts

	p.workers = hosts.Filter(func(h *cluster.Host) bool {
		return !h.Metadata.EngineUploaded && !h.IsController()
	})

	p.control = hosts.Filter(func(h *cluster.Host) bool {
		return !h.Metadata.EngineUploaded && h.IsController()
	})

	p.priorManifest = make(map[*cluster.Host][]ManifestEntry, len(p.control)+len(p.workers))
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
		p.blockOtherInstallsIfUploaded(p.filesControl),
	)
	if err != nil {
		return err
	}
	err = p.parallelDoUpload(
		ctx,
		p.workers,
		p.uploadWorkerFiles,
		p.blockOtherInstallsIfUploaded(p.filesWorkers),
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
func (p *UploadFilesCommon) cleanStaleUploads(ctx context.Context, h *cluster.Host) error {
	current := ParseManifest(strings.Join(h.Metadata.UploadedFiles, "\n"))
	old := filterManifestByCategory(p.priorManifest[h], "engine")
	current = filterManifestByCategory(current, "engine")
	p.removeStaleManifestEntries(ctx, h, old, current)
	return nil
}

// blockOtherInstallsIfUploaded marks a host populated only when this phase actually had files for
// it. A host the package carries no files for (e.g. a Debian host and a package with no deb
// selector) is left unmarked, so a later catch-all phase like BIN still sees it.
func (p *UploadFilesCommon) blockOtherInstallsIfUploaded(byArch map[api.Arch][]v1alpha1.File) func(context.Context, *cluster.Host) error {
	return func(ctx context.Context, h *cluster.Host) error {
		files, err := p.filesFor(byArch, h)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			logger.From(ctx).Debug("no files uploaded for this host, leaving it open to other install phases", "host", h)
			return nil
		}
		logger.From(ctx).Debug("disabling host from other installs", "host", h)
		h.Metadata.EngineUploaded = true
		return nil
	}
}

func (p *UploadFilesCommon) uploadControllerFiles(ctx context.Context, h *cluster.Host) error {
	files, err := p.filesFor(p.filesControl, h)
	if err != nil {
		return err
	}

	return p.uploadFiles(ctx, h, files)
}

func (p *UploadFilesCommon) uploadWorkerFiles(ctx context.Context, h *cluster.Host) error {
	files, err := p.filesFor(p.filesWorkers, h)
	if err != nil {
		return err
	}

	return p.uploadFiles(ctx, h, files)
}

// filesFor picks the files built for a host's own architecture.
//
// A host whose architecture cannot be read fails rather than taking no files. Uploading nothing is
// not the same as doing nothing here: cleanStaleUploads diffs this run's engine manifest against
// the one the host already carried, so a host that recorded no engine files this run reads as a
// version that dropped them, and the engine binaries it is running are deleted.
//
// ValidateHosts rejects a host the package does not carry, but only for a package that records
// which architectures it was built for. One built before that metadata existed is applied
// unchecked, so an architecture cargoship cannot parse still reaches this.
func (p *UploadFilesCommon) filesFor(byArch map[api.Arch][]v1alpha1.File, h *cluster.Host) ([]v1alpha1.File, error) {
	arch, err := hostArch(h)
	if err != nil {
		return nil, fmt.Errorf("cannot tell which files %s needs: %w", h, err)
	}

	return byArch[arch], nil
}

// installPackagesFor installs the packages built for a host's own architecture.
//
// A host the package carries no packages for is left alone rather than handed an empty list: dnf
// and apt-get both read a list of packages to install, and calling either with none fails on its
// own usage, which says nothing about why cargoship had no packages to give it.
func (p *UploadFilesCommon) installPackagesFor(ctx context.Context, byArch map[api.Arch][]v1alpha1.File, h *cluster.Host) error {
	files, err := p.filesFor(byArch, h)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		logger.From(ctx).Warn("the package carries no engine packages for this host architecture, skipping the install", "host", h)
		return nil
	}

	return h.Configurer.InstallPackage(h, getPath(files)...)
}

// ShouldRun is true when there are workers
func (p *UploadFilesCommon) ShouldRun() bool {
	return (len(p.control) + len(p.workers)) > 0
}

// getProfileFiles groups the files this phase uploads by the architecture of the host receiving
// them, so a cluster of mixed CPUs gets the right binaries on each node.
func (p *UploadFilesCommon) getProfileFiles(ctx context.Context, selector string, profile string) map[api.Arch][]v1alpha1.File {
	// p.control and p.workers are read as they stand, so a phase that narrows them first, as the
	// RPM and APT phases do, only builds file sets for architectures it will actually upload.
	arches := hostArches(ctx, slices.Concat(p.control, p.workers))
	byArch := make(map[api.Arch][]v1alpha1.File, len(arches))

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
func (p *UploadFilesCommon) profileFilesForArch(ctx context.Context, selector string, profile string, arch api.Arch) []v1alpha1.File {
	files := []v1alpha1.File{}

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
		if !whenEnabled(ctx, p.manager.Values, f.Selector.When, filepath.Base(f.Target)) {
			continue
		}

		logger.From(ctx).Debug("determined this file needs to be uploaded", "file", filepath.Base(f.Target), "arch", arch)
		filePath := filepath.Join(p.manager.TempDirectory, config.OSDir, strconv.Itoa(i), filepath.Base(f.Target))
		err := os.Chtimes(filePath, time.Unix(0, 0), time.Unix(0, 0))
		if err != nil {
			logger.From(ctx).Warn("failed to change the file time", "error", err)
		}
		// Only the BIN selector stages: phase 59 is the one install phase that renames a
		// staged file into place, and it stages so that an upgrade swaps a running engine
		// binary atomically rather than writing over it mid-upload. An rpm or apt selector
		// has no such step, so a file staged there would sit under its .tmp.<nonce> name
		// until the next run's cleanUpOldTmpFiles deleted it.
		target := f.Target
		if f.Executable && selector == config.SelectorBIN {
			target = stageTempPath(false, f.Target)
		}
		files = append(files, v1alpha1.File{
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
	err := p.parallelDo(context.Background(), p.manager.Config.Spec.Hosts, func(_ context.Context, h *cluster.Host) error {
		if len(h.Metadata.BinaryTempFile) == 0 {
			return nil
		}
		logger.From(ctx).Info("cleaning up binary tempfile", "host", h)
		for _, f := range h.Metadata.BinaryTempFile {
			logger.From(ctx).Debug("removing file", "file", f, "host", h)
			if err := h.DeleteFile(f); err != nil {
				logger.From(ctx).Warn("failed to delete", "host", h, "file", f, "error", err)
			}
		}
		return nil
	})
	if err != nil {
		logger.From(ctx).Warn("failed to clean up tempfiles")
	}
}
