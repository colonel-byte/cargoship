// Copyright 2021 zarf authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from zarf:
// https://github.com/zarf-dev/zarf
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

// Package assemble builds a Cargoship package on disk
package assemble

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/colonel-byte/cargoship/src/api"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/pkg/helpers"
	"github.com/colonel-byte/cargoship/src/pkg/images"
	"github.com/colonel-byte/cargoship/src/pkg/packager/layout"
	"github.com/colonel-byte/cargoship/src/pkg/utils"
	goyaml "github.com/goccy/go-yaml"
	zlang "github.com/zarf-dev/zarf/src/config/lang"
	"github.com/zarf-dev/zarf/src/pkg/archive"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/packager/actions"
	"github.com/zarf-dev/zarf/src/pkg/template"
	"github.com/zarf-dev/zarf/src/pkg/transform"
	"github.com/zarf-dev/zarf/src/types"
)

// AssembleOptions options
type AssembleOptions struct {
	RegistryOverrides []images.RegistryOverride
	OCIConcurrency    int
	CachePath         string
	SkipSBOM          bool
	types.RemoteOptions
}

// AssembleDistro creates the actual tarballs
func AssembleDistro(ctx context.Context, d distro.ZarfDistro, distroPath string, opts AssembleOptions) (*layout.DistroLayout, error) {
	l := logger.From(ctx)
	l.Info("assembling distro", "path", distroPath)

	buildPath, err := utils.MakeTempDir(config.CommonOptions.TempDirectory)
	if err != nil {
		return nil, err
	}
	l.Debug("assembling distro in temp folder", "tmp", buildPath)

	onCreate := d.Spec.Actions.OnCreate

	if err := actions.Run(ctx, distroPath, onCreate.Defaults, onCreate.Before, nil, nil, template.StateAccess{}); err != nil {
		return nil, fmt.Errorf("unable to run component before action: %w", err)
	}

	for filesIdx, file := range d.Spec.Config.Files {
		err := fileGrabber(ctx, string(config.FilesDir), buildPath, distroPath, filesIdx, *file)
		if err != nil {
			logger.From(ctx).Warn("got", "error", err)
		}
	}
	for filesIdx, file := range d.Spec.Config.OS.Files {
		err := fileGrabber(ctx, string(config.OSDir), buildPath, distroPath, filesIdx, *file)
		if err != nil {
			logger.From(ctx).Warn("got", "error", err)
		}
	}

	componentImages := []transform.Image{}
	for _, src := range d.Spec.Config.ImagesConfig.Images {
		refInfo, err := transform.ParseImageRef(src)
		if err != nil {
			return nil, fmt.Errorf("failed to create ref for image %s: %w", src, err)
		}
		if slices.Contains(componentImages, refInfo) {
			continue
		}
		componentImages = append(componentImages, refInfo)
	}

	if len(componentImages) > 0 {
		pullOpts := images.PullOptions{
			OCIConcurrency:        opts.OCIConcurrency,
			Arch:                  d.Metadata.Architecture,
			RegistryOverrides:     opts.RegistryOverrides,
			CacheDirectory:        filepath.Join(opts.CachePath, config.ImagesDir),
			PlainHTTP:             opts.PlainHTTP,
			InsecureSkipTLSVerify: opts.InsecureSkipTLSVerify,
		}
		l.Info("pulling images too", "path", filepath.Join(buildPath, config.ImagesDir))
		_, err := images.Pull(ctx, componentImages, filepath.Join(buildPath, config.ImagesDir), pullOpts)
		if err != nil {
			return nil, err
		}
	}

	if err := actions.Run(ctx, distroPath, onCreate.Defaults, onCreate.After, nil, nil, template.StateAccess{}); err != nil {
		return nil, fmt.Errorf("unable to run component before action: %w", err)
	}

	checksumContent, checksumSha, err := getChecksum(buildPath)
	if err != nil {
		return nil, err
	}
	checksumPath := filepath.Join(buildPath, config.Checksums)
	err = os.WriteFile(checksumPath, []byte(checksumContent), helpers.ReadWriteUser)
	if err != nil {
		return nil, err
	}
	d.Metadata.AggregateChecksum = checksumSha

	d = recordDistroMetadata(d, opts.RegistryOverrides)

	b, err := goyaml.Marshal(d)
	if err != nil {
		return nil, err
	}
	err = os.WriteFile(filepath.Join(buildPath, config.DistroYAML), b, helpers.ReadWriteUser)
	if err != nil {
		return nil, err
	}

	return layout.NewDistroLayout(buildPath, d), nil
}

func fileGrabber(ctx context.Context, resourceType string, buildPath string, distroPath string, filesIdx int, file v1alpha1.ZarfFile) error {
	rel := filepath.Join(resourceType, strconv.Itoa(filesIdx), filepath.Base(file.Target))
	dst := filepath.Join(buildPath, rel)
	destinationDir := filepath.Dir(dst)

	if helpers.IsURL(file.Source) {
		if file.ExtractPath != "" {
			// get the compressedFileName from the source
			compressedFileName, err := helpers.ExtractBasePathFromURL(file.Source)
			if err != nil {
				return fmt.Errorf(zlang.ErrFileNameExtract, file.Source, err)
			}
			tmpDir, err := utils.MakeTempDir(config.CommonOptions.TempDirectory)
			if err != nil {
				return err
			}
			defer func() {
				err = errors.Join(err, os.RemoveAll(tmpDir))
			}()
			compressedFile := filepath.Join(tmpDir, compressedFileName)

			// If the file is an archive, download it to the componentPath.Temp
			if err := utils.DownloadToFileWithChecksum(ctx, file.Source, compressedFile, file.Shasum, filepath.Base(file.Target)); err != nil {
				return fmt.Errorf(zlang.ErrDownloading, file.Source, err)
			}
			decompressOpts := archive.DecompressOpts{
				Files: []string{file.ExtractPath},
			}
			err = archive.Decompress(ctx, compressedFile, destinationDir, decompressOpts)
			if err != nil {
				return fmt.Errorf(zlang.ErrFileExtract, file.ExtractPath, compressedFileName, err)
			}
		} else {
			if err := utils.DownloadToFileWithChecksum(ctx, file.Source, dst, file.Shasum, filepath.Base(file.Target)); err != nil {
				return fmt.Errorf(zlang.ErrDownloading, file.Source, err)
			}
		}
	} else {
		src := file.Source
		if !filepath.IsAbs(file.Source) {
			src = filepath.Join(distroPath, file.Source)
		}
		if file.ExtractPath != "" {
			decompressOpts := archive.DecompressOpts{
				Files: []string{file.ExtractPath},
			}
			err := archive.Decompress(ctx, src, destinationDir, decompressOpts)
			if err != nil {
				return fmt.Errorf(zlang.ErrFileExtract, file.ExtractPath, src, err)
			}
		} else {
			if err := helpers.CreatePathAndCopy(src, dst); err != nil {
				return fmt.Errorf("unable to copy file %s: %w", src, err)
			}
		}
	}

	if file.ExtractPath != "" {
		// Make sure dst reflects the actual file or directory.
		updatedExtractedFileOrDir := filepath.Join(destinationDir, file.ExtractPath)
		if updatedExtractedFileOrDir != dst {
			if err := os.Rename(updatedExtractedFileOrDir, dst); err != nil {
				return fmt.Errorf(zlang.ErrWritingFile, dst, err)
			}
		}
	}

	// Abort packaging on invalid shasum (if one is specified).
	if file.Shasum != "" {
		if err := helpers.SHAsMatch(dst, file.Shasum); err != nil {
			return fmt.Errorf("sha mismatch for %s: %w", file.Source, err)
		}
	}

	if file.Executable || helpers.IsDir(dst) {
		err := os.Chmod(dst, helpers.ReadWriteExecuteUser)
		if err != nil {
			return err
		}
	} else {
		err := os.Chmod(dst, helpers.ReadWriteUser)
		if err != nil {
			return err
		}
	}
	return nil
}

func recordDistroMetadata(distro distro.ZarfDistro, registryOverrides []images.RegistryOverride) distro.ZarfDistro {
	now := time.Now()
	distro.Build.Architecture = distro.Metadata.Architecture
	distro.Build.Timestamp = now.Format(api.BuildTimestampFormat)
	distro.Build.Version = distro.Metadata.Version

	overrides := make(map[string]string, len(registryOverrides))
	for i := range registryOverrides {
		overrides[registryOverrides[i].Source] = registryOverrides[i].Override
	}

	distro.Build.RegistryOverrides = overrides

	return distro
}

func getChecksum(dirPath string) (string, string, error) {
	checksumData := []string{}
	err := filepath.Walk(dirPath, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dirPath, path)
		if err != nil {
			return err
		}
		if rel == config.DistroYAML || rel == config.Checksums {
			return nil
		}
		sum, err := helpers.GetSHA256OfFile(path)
		if err != nil {
			return err
		}
		checksumData = append(checksumData, fmt.Sprintf("%s %s", sum, filepath.ToSlash(rel)))
		return nil
	})
	if err != nil {
		return "", "", err
	}
	slices.Sort(checksumData)

	checksumContent := strings.Join(checksumData, "\n") + "\n"
	sha := sha256.Sum256([]byte(checksumContent))
	return checksumContent, hex.EncodeToString(sha[:]), nil
}
