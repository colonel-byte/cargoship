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

package packager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/colonel-byte/zarf-distro/src/api/v1alpha1"
	"github.com/colonel-byte/zarf-distro/src/config"
	"github.com/zarf-dev/zarf/src/pkg/archive"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type DistroLayout struct {
	dirPath string
	Distro  v1alpha1.ZarfDistroPackage
}

func NewDistroLayout(dir string, distro v1alpha1.ZarfDistroPackage) *DistroLayout {
	return &DistroLayout{
		dirPath: dir,
		Distro:  distro,
	}
}

func (d *DistroLayout) DirPath() string {
	return d.dirPath
}

// Cleanup removes any temporary directories created.
func (d *DistroLayout) Cleanup() error {
	err := os.RemoveAll(d.dirPath)
	if err != nil {
		return err
	}
	return nil
}

func (d *DistroLayout) GetImageDirPath() string {
	// Use the manifest within the index.json to load the specific image we want
	return filepath.Join(d.dirPath, config.ImagesDir)
}

// FileName returns the name of the Zarf package should have when exported to the file system
func (d *DistroLayout) FileName() (string, error) {
	if d.Distro.Build.Architecture == "" {
		return "", errors.New("package must include a build architecture")
	}
	arch := d.Distro.Build.Architecture

	name := fmt.Sprintf("zarf-distro-%s", arch)
	if d.Distro.Metadata.Version != "" {
		name = fmt.Sprintf("%s-%s", name, d.Distro.Metadata.Version)
	}

	if d.Distro.Metadata.Uncompressed {
		return name + ".tar", nil
	}
	return name + ".tar.zst", nil
}

// Archive creates a tarball from the package layout and returns the path to that tarball
func (d *DistroLayout) Archive(ctx context.Context, dirPath string, maxPackageSize int) (string, error) {
	filename, err := d.FileName()
	if err != nil {
		return "", err
	}
	tarballPath := filepath.Join(dirPath, filename)
	err = os.Remove(tarballPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	logger.From(ctx).Info("writing package to disk", "path", tarballPath)

	files, err := os.ReadDir(d.dirPath)
	if err != nil {
		return "", err
	}
	var filePaths []string
	for _, file := range files {
		filePaths = append(filePaths, filepath.Join(d.dirPath, file.Name()))
	}
	err = archive.Compress(ctx, filePaths, tarballPath, archive.CompressOpts{})
	_, err = os.Stat(tarballPath)
	if err != nil {
		return "", fmt.Errorf("unable to read the package archive: %w", err)
	}

	return tarballPath, nil
}
