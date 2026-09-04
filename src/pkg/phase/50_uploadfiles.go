// Copyright 2023 k0sctl authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from k0sctl:
// https://github.com/k0sproject/k0sctl
//
// Modifications Copyright 2026 colonel-byte.
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
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/config"
	carch "github.com/colonel-byte/cargoship/src/pkg/oci/archive"
	"github.com/containerd/containerd/v2/core/images/archive"
	"github.com/containerd/platforms"
	"github.com/k0sproject/rig/exec"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/pkg/images"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"
)

// UploadFiles implements a phase which upload files to hosts
type UploadFiles struct {
	GenericPhase

	hosts    cluster.ZarfHosts
	disFiles v1alpha1.ZarfFiles
	imgFiles []v1alpha1.ZarfFile

	// priorManifest holds each host's upload manifest as it was before this run touched it,
	// captured in Prepare so Run can tell which images an upgrade no longer uploads (e.g. an
	// image tag that was removed from the distro config, or renamed to a new version).
	priorManifest map[*cluster.ZarfHost][]ManifestEntry
}

// Title for the phase
func (p *UploadFiles) Title() string {
	return "Upload files to hosts"
}

// Explanation about the current phase, used for documentation generation
func (p *UploadFiles) Explanation() string {
	return "Uploads the distro agnostic files to each remote node"
}

var (
	tagPrefix = regexp.MustCompile(`:.+$`)
	nsPrefix  = regexp.MustCompile(`/`)
)

// Prepare the phase
func (p *UploadFiles) Prepare(ctx context.Context, c *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	p.manager.Config = c
	p.hosts = p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return (len(h.Files) + len(d.Spec.Config.Files) + len(p.manager.Distro.Spec.Config.ImagesConfig.Images)) > 0
	})
	p.disFiles = p.manager.Distro.Spec.Config.Files

	p.priorManifest = make(map[*cluster.ZarfHost][]ManifestEntry, len(p.manager.Config.Spec.Hosts))
	for _, h := range p.manager.Config.Spec.Hosts {
		p.priorManifest[h] = p.readManifest(h)
	}

	err := os.MkdirAll(filepath.Join(p.manager.TempDirectory, config.TarBallDir), 0755)
	if err != nil {
		return err
	}

	imagesPath := filepath.Join(p.manager.TempDirectory, config.ImagesDir)
	src, err := oci.NewWithContext(ctx, imagesPath)
	if err != nil {
		return err
	}

	store := &carch.OciArchiveStore{Root: imagesPath, Src: src}

	compression := p.manager.Distro.Spec.Config.ImagesConfig.Compression
	tarSuffix, err := p.manager.Distro.Spec.Config.ImagesConfig.TarballSuffix()
	if err != nil {
		return err
	}

	// TODO(#258): DefaultStrict is the architecture cargoship itself runs on. Apply time selection
	// has to use the architecture of the host being uploaded to, which is a per host tarball.
	hostPlatform := platforms.DefaultStrict()

	for _, i := range p.manager.Distro.Spec.Config.ImagesConfig.Images {
		tarBallName := tagPrefix.ReplaceAllLiteralString(nsPrefix.ReplaceAllLiteralString(i, "_"), tarSuffix)
		tarballPath := filepath.Join(p.manager.TempDirectory, config.TarBallDir, tarBallName)

		desc, err := src.Resolve(ctx, i)
		if err != nil {
			return err
		}

		desc, err = resolveImageManifest(ctx, src, desc, hostPlatform)
		if err != nil {
			return fmt.Errorf("failed to select a manifest for image %s: %w", i, err)
		}

		desc.URLs = []string{
			i,
		}

		writer, err := os.OpenFile(tarballPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}

		compressor, err := imageCompressWriter(compression, writer)
		if err != nil {
			if cerr := writer.Close(); cerr != nil {
				logger.From(ctx).Warn("failed to close writer", "error", cerr)
			}
			return err
		}

		err = archive.Export(
			ctx,
			store,
			compressor,
			archive.WithManifest(desc, i),
			archive.WithPlatform(hostPlatform),
		)
		if err != nil {
			logger.From(ctx).Warn("failed to create archive", "error", err)
		}

		// The compressor has to be closed before the file so its trailer lands in the tarball.
		err = compressor.Close()
		if err != nil {
			logger.From(ctx).Warn("failed to close compressor", "compression", compression, "error", err)
		}

		err = writer.Close()
		if err != nil {
			logger.From(ctx).Warn("failed to close writer", "error", err)
		}

		err = os.Chtimes(tarballPath, time.Unix(0, 0), time.Unix(0, 0))
		if err != nil {
			return err
		}

		p.imgFiles = append(p.imgFiles, v1alpha1.ZarfFile{
			Name:        tarBallName,
			Target:      p.manager.Distro.Spec.Config.ImagesConfig.Path,
			TargetIsDir: true,
			Category:    "image",
			LocalSource: v1alpha1.LocalFile{
				Path: tarballPath,
			},
		})
	}

	return nil
}

// resolveImageManifest picks the manifest to export for an image. An image pulled for a single
// architecture is tagged as a manifest and comes back unchanged. An image pulled for several is
// tagged as an index, so the manifest matching the platform is chosen out of it here rather than
// left to archive.Export: the exporter filters an index's children by platform but still writes the
// whole index blob into the tarball, which would leave the tarball referencing blobs for a platform
// it deliberately left out.
func resolveImageManifest(ctx context.Context, src *oci.Store, desc ocispec.Descriptor, matcher platforms.MatchComparer) (ocispec.Descriptor, error) {
	if !images.IsIndex(desc.MediaType) {
		return desc, nil
	}

	indexBytes, err := content.FetchAll(ctx, src, desc)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("failed to read image index %s: %w", desc.Digest, err)
	}
	var index ocispec.Index
	if err := json.Unmarshal(indexBytes, &index); err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("failed to parse image index %s: %w", desc.Digest, err)
	}

	matches := []ocispec.Descriptor{}
	available := []string{}
	for _, child := range index.Manifests {
		if child.Platform == nil {
			continue
		}
		available = append(available, platforms.Format(*child.Platform))
		if matcher.Match(*child.Platform) {
			matches = append(matches, child)
		}
	}
	if len(matches) == 0 {
		return ocispec.Descriptor{}, fmt.Errorf("image index %s holds no manifest for this platform, it holds %s",
			desc.Digest, strings.Join(available, ", "))
	}
	// An index can hold more than one manifest a platform accepts, for example an arm64 variant the
	// host can run alongside the plain arm64 build, so the matcher orders them by preference.
	sort.SliceStable(matches, func(i, j int) bool {
		return matcher.Less(*matches[i].Platform, *matches[j].Platform)
	})

	return matches[0], nil
}

// ShouldRun is true when there are workers
func (p *UploadFiles) ShouldRun() bool {
	return len(p.hosts) > 0
}

// Run the phase
func (p *UploadFiles) Run(ctx context.Context) error {
	logger.From(ctx).Info("needing to upload files", "count", len(p.disFiles))
	logger.From(ctx).Info("needing to upload images", "count", len(p.imgFiles))

	return p.parallelDoUpload(
		ctx,
		p.manager.Config.Spec.Hosts,
		p.cleanUpOldTmpFiles,
		p.uploadDistroFiles,
		p.cleanStaleUploads,
	)
}

// cleanStaleUploads removes images this run's upload left in the manifest from a previous
// version but didn't re-upload itself, e.g. an image tag bumped by a version upgrade. The diff
// is scoped to the "image" category: the engine binary phases run after this one and haven't
// recorded their own files into h.Metadata.UploadedFiles yet, so comparing the full manifest
// here would misread their files as stale and delete a still-in-use engine binary.
func (p *UploadFiles) cleanStaleUploads(ctx context.Context, h *cluster.ZarfHost) error {
	current := parseManifest(strings.Join(h.Metadata.UploadedFiles, "\n"))
	old := filterManifestByCategory(p.priorManifest[h], "image")
	current = filterManifestByCategory(current, "image")
	p.removeStaleManifestEntries(ctx, h, old, current)
	return nil
}

func (p *UploadFiles) cleanUpOldTmpFiles(ctx context.Context, h *cluster.ZarfHost) error {
	l := logger.From(ctx)

	files := slices.Concat(p.disFiles, p.manager.Distro.Spec.Config.OS.Files)

	for _, f := range files {
		file := filepath.Base(f.Target)
		binary := fmt.Sprintf("%s.tmp.*", file)
		re := regexp.MustCompile(binary)
		folder := filepath.Dir(f.Target)
		if f.TargetIsDir {
			folder = f.Target
		}
		err := fs.WalkDir(h.SudoFsys(), folder, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				l.Debug(fmt.Sprintf("failed to walk %s", binary), "path", file, "error", err)
				return nil
			}
			if !d.IsDir() && re.MatchString(d.Name()) {
				l.Debug("cleaning up old engine binary upload temporary file", "host", h, "path", path)
				if err := h.Configurer.DeleteFile(h, path); err != nil {
					l.Warn("failed to delete", "host", h, "path", path, "error", err)
				}
				return nil
			}
			return nil
		})
		if err != nil {
			l.Warn(fmt.Sprintf("failed to walk %s", binary), "path", file, "error", err)
		}
	}
	return nil
}

func (p *UploadFiles) uploadDistroFiles(ctx context.Context, h *cluster.ZarfHost) error {
	files := []v1alpha1.ZarfFile{}

	for i, f := range p.disFiles {
		if ctx.Err() != nil {
			return fmt.Errorf("upload canceled: %w", ctx.Err())
		}
		target := f.Target
		if f.Executable {
			target = stageTempPath(h.IsWindows(), f.Target)
			f.OriginalTarget = target
		}
		logger.From(ctx).Debug("need to upload from distro package", "source", filepath.Join(p.manager.TempDirectory, config.FilesDir, strconv.Itoa(i), filepath.Base(f.Target)), "target", target)
		files = append(files, v1alpha1.ZarfFile{
			Name:           filepath.Base(f.Target),
			Target:         target,
			OriginalTarget: f.Target,
			TargetIsDir:    f.TargetIsDir,
			Category:       "file",
			LocalSource: v1alpha1.LocalFile{
				Path: filepath.Join(p.manager.TempDirectory, config.FilesDir, strconv.Itoa(i), filepath.Base(f.Target)),
			},
		})
	}
	for _, f := range h.Files {
		if ctx.Err() != nil {
			return fmt.Errorf("upload canceled: %w", ctx.Err())
		}
		logger.From(ctx).Debug("need to upload", "target", f.Destination)
		if f.Data != "" {
			err := p.uploadData(ctx, h, &v1alpha1.ZarfFile{
				Name:   filepath.Base(f.Destination),
				Target: f.Destination,
				Data:   f.Data,
			})
			if err != nil {
				logger.From(ctx).Warn("failed to upload data", "file", f.Destination)
			}
		}
	}

	for i, f := range files {
		logger.From(ctx).Debug("file", "num", i+1, "count", len(files))
		if err := p.uploadFile(ctx, h, &f); err != nil {
			logger.From(ctx).Warn("failed to upload", "file", f.Name, "host", h, "error", err)
		}
		if f.Executable {
			if err := h.Exec(fmt.Sprintf("chmod +x %s", f.Target), exec.Sudo(h)); err != nil {
				logger.From(ctx).Warn("failed to add execute permission", "file", f, "host", h)
			}
		}
	}

	for i, f := range p.imgFiles {
		logger.From(ctx).Debug("image", "num", i+1, "count", len(p.imgFiles))
		if err := p.uploadFile(ctx, h, &f); err != nil {
			logger.From(ctx).Warn("failed to upload", "file", f.Name, "host", h, "error", err)
		}
	}

	return nil
}
