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
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/config"
	"github.com/containerd/containerd/v2/core/images/archive"
	"github.com/containerd/containerd/v2/plugins/content/local"
	"github.com/containerd/platforms"
	"github.com/k0sproject/rig/exec"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"oras.land/oras-go/v2/content/oci"
)

// UploadFiles implements a phase which upload files to hosts
type UploadFiles struct {
	GenericPhase

	hosts    cluster.ZarfHosts
	disFiles v1alpha1.ZarfFiles
	imgFiles []v1alpha1.ZarfFile
}

// Title for the phase
func (p *UploadFiles) Title() string {
	return "Upload files to hosts"
}

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

	err := os.MkdirAll(filepath.Join(p.manager.TempDirectory, config.TarBallDir), 0755)
	if err != nil {
		return err
	}

	src, err := oci.NewWithContext(ctx, filepath.Join(p.manager.TempDirectory, config.ImagesDir))
	if err != nil {
		return err
	}

	store, err := local.NewStore(filepath.Join(p.manager.TempDirectory, config.ImagesDir))
	if err != nil {
		return err
	}

	for _, i := range p.manager.Distro.Spec.Config.ImagesConfig.Images {
		tarBallName := tagPrefix.ReplaceAllLiteralString(nsPrefix.ReplaceAllLiteralString(i, "_"), ".tar")
		tarballPath := filepath.Join(p.manager.TempDirectory, config.TarBallDir, tarBallName)

		desc, err := src.Resolve(ctx, i)
		if err != nil {
			return err
		}

		desc.URLs = []string{
			i,
		}

		writer, err := os.OpenFile(tarballPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}

		err = archive.Export(
			ctx,
			store,
			writer,
			archive.WithManifest(desc, i),
			archive.WithPlatform(platforms.DefaultStrict()),
		)
		if err != nil {
			logger.From(ctx).Warn("failed to create archive", "error", err)
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
			LocalSource: v1alpha1.LocalFile{
				Path: tarballPath,
			},
		})
	}

	return nil
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
	)
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
			logger.From(ctx).Warn("failed to upload", "file", f, "host", h)
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
			logger.From(ctx).Warn("failed to upload", "file", f, "host", h)
		}
	}

	return nil
}
