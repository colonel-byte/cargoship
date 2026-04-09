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
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/zarf-distro/src/config"
	"github.com/zarf-dev/zarf/src/pkg/archive"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	zutils "github.com/zarf-dev/zarf/src/pkg/utils"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/oci"
)

// UploadFiles implements a phase which upload files to hosts
type UploadFiles struct {
	GenericPhase

	hosts    cluster.ZarfHosts
	disFiles []distro.ZarfFile
	imgFiles []cluster.UploadFile
}

// Title for the phase
func (p *UploadFiles) Title() string {
	return "Upload files to hosts"
}

var (
	tagPrefix    = regexp.MustCompile(`:.+$`)
	nsPrefix     = regexp.MustCompile(`/`)
	tarBallRegex = regexp.MustCompile(`.+\.tar$`)
)

// Prepare the phase
func (p *UploadFiles) Prepare(ctx context.Context, c *cluster.ZarfCluster, d *distro.ZarfDistro) error {
	p.manager.Config = c
	p.hosts = p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return (len(h.Files) + len(d.Spec.Config.Files)) > 0
	})
	p.disFiles = p.manager.Distro.Spec.Config.Files

	for _, i := range p.manager.Distro.Spec.Config.ImagesConfig.Images {
		buildPath, err := zutils.MakeTempDir(config.CommonOptions.TempDirectory)
		if err != nil {
			return err
		}
		defer os.RemoveAll(buildPath)

		tarBallName := tagPrefix.ReplaceAllLiteralString(nsPrefix.ReplaceAllLiteralString(i, "_"), ".tar")
		src, err := oci.NewWithContext(ctx, filepath.Join(p.manager.TempDirectory, config.ImagesDir))
		if err != nil {
			return err
		}
		dst, err := oci.NewWithContext(ctx, buildPath)
		if err != nil {
			return err
		}
		_, err = oras.Copy(ctx, src, i, dst, i, oras.DefaultCopyOptions)
		if err != nil {
			return err
		}
		tarballPath := filepath.Join(p.manager.TempDirectory, config.TarBallDir, tarBallName)
		err = os.Remove(tarballPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		logger.From(ctx).Debug("writing package to disk", "path", tarballPath)
		files, err := os.ReadDir(buildPath)
		if err != nil {
			return err
		}
		var filePaths []string
		for _, file := range files {
			filePaths = append(filePaths, filepath.Join(buildPath, file.Name()))
		}
		err = archive.Compress(ctx, filePaths, tarballPath, archive.CompressOpts{})
		_, err = os.Stat(tarballPath)
		if err != nil {
			return fmt.Errorf("unable to read the package archive: %w", err)
		}

		p.imgFiles = append(p.imgFiles, cluster.UploadFile{
			Name:           tarBallName,
			DestinationDir: p.manager.Distro.Spec.Config.ImagesConfig.Path,
			Sources: []*cluster.LocalFile{
				{
					Path: tarballPath,
				},
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
		p.cleanUpOldImageFiles,
		p.uploadDistroFiles,
	)
}

func (p *UploadFiles) cleanUpOldTmpFiles(ctx context.Context, h *cluster.ZarfHost) error {
	l := logger.From(ctx)

	for _, f := range p.disFiles {
		file := filepath.Base(f.Target)
		binary := fmt.Sprintf("%s.tmp.*", file)
		re := regexp.MustCompile(binary)
		err := fs.WalkDir(h.SudoFsys(), filepath.Dir(f.Target), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				l.Warn(fmt.Sprintf("failed to walk %s", binary), "path", file, "error", err)
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

func (p *UploadFiles) cleanUpOldImageFiles(ctx context.Context, h *cluster.ZarfHost) error {
	l := logger.From(ctx)

	file := p.manager.Distro.Spec.Config.ImagesConfig.Path
	err := fs.WalkDir(h.SudoFsys(), p.manager.Distro.Spec.Config.ImagesConfig.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			l.Warn(fmt.Sprintf("failed to walk %s", file), "path", file, "error", err)
			return nil
		}
		if !d.IsDir() && tarBallRegex.MatchString(path) {
			l.Debug("removing old image file", "host", h, "path", path)
			if err := h.Configurer.DeleteFile(h, path); err != nil {
				l.Warn("failed to delete", "host", h, "path", path, "error", err)
			}
			return nil
		}
		return nil
	})
	if err != nil {
		l.Warn(fmt.Sprintf("failed to walk %s", file), "path", file, "error", err)
	}
	return nil
}

func (p *UploadFiles) uploadDistroFiles(ctx context.Context, h *cluster.ZarfHost) error {
	files := []cluster.UploadFile{}

	for i, f := range p.disFiles {
		if ctx.Err() != nil {
			return fmt.Errorf("upload canceled: %w", ctx.Err())
		}
		stagingFile := stageTempPath(*h, f.Target)
		logger.From(ctx).Debug("need to upload from distro package", "source", filepath.Join(p.manager.TempDirectory, config.FilesDir, strconv.Itoa(i), filepath.Base(f.Target)), "target", stagingFile)
		files = append(files, cluster.UploadFile{
			Name:            filepath.Base(f.Target),
			DestinationFile: stagingFile,
			Sources: []*cluster.LocalFile{
				{
					Path: filepath.Join(p.manager.TempDirectory, config.FilesDir, strconv.Itoa(i), filepath.Base(f.Target)),
				},
			},
		})
	}
	for _, f := range h.Files {
		if ctx.Err() != nil {
			return fmt.Errorf("upload canceled: %w", ctx.Err())
		}
		logger.From(ctx).Debug("need to upload", "target", f.Destination)
		if f.Data != "" {
			p.uploadData(ctx, h, &cluster.UploadFile{
				Name:            filepath.Base(f.Destination),
				DestinationFile: f.Destination,
				Data:            f.Data,
			})
		}
	}

	for i, f := range files {
		logger.From(ctx).Debug("file", "num", i+1, "count", len(files))
		p.uploadFile(ctx, h, &f)
	}

	for i, f := range p.imgFiles {
		logger.From(ctx).Debug("image", "num", i+1, "count", len(p.imgFiles))
		p.uploadFile(ctx, h, &f)
	}

	return nil
}
