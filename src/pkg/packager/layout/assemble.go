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

package layout

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/colonel-byte/cargoship/src/api"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/config"
	"github.com/defenseunicorns/pkg/helpers/v2"
	goyaml "github.com/goccy/go-yaml"
	zlang "github.com/zarf-dev/zarf/src/config/lang"
	"github.com/zarf-dev/zarf/src/pkg/archive"
	"github.com/zarf-dev/zarf/src/pkg/images"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/packager/actions"
	"github.com/zarf-dev/zarf/src/pkg/transform"
	zutils "github.com/zarf-dev/zarf/src/pkg/utils"
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
func AssembleDistro(ctx context.Context, d distro.ZarfDistro, distroPath string, opts AssembleOptions) (*DistroLayout, error) {
	l := logger.From(ctx)
	l.Info("assembling distro", "path", distroPath)

	buildPath, err := zutils.MakeTempDir(config.CommonOptions.TempDirectory)
	if err != nil {
		return nil, err
	}
	l.Debug("assembling distro in temp folder", "tmp", buildPath)

	onCreate := d.Spec.Actions.OnCreate

	if err := actions.Run(ctx, distroPath, onCreate.Defaults, onCreate.Before, nil, nil); err != nil {
		return nil, fmt.Errorf("unable to run component before action: %w", err)
	}

	for filesIdx, file := range d.Spec.Config.Files {
		fileGrabber(ctx, string(config.FilesDir), buildPath, distroPath, filesIdx, *file)
	}
	for filesIdx, file := range d.Spec.Config.OS.Files {
		fileGrabber(ctx, string(config.OSDir), buildPath, distroPath, filesIdx, *file)
	}

	componentImages := []transform.Image{}
	manifests := []images.ImageWithManifest{}
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
		imageManifests, err := images.Pull(ctx, componentImages, filepath.Join(buildPath, config.ImagesDir), pullOpts)
		if err != nil {
			return nil, err
		}
		manifests = append(manifests, imageManifests...)
	}
	// TODO add in the sbom logic
	// sbomImageList := []transform.Image{}
	// for _, manifest := range manifests {
	// 	ok := images.OnlyHasImageLayers(manifest.Manifest)
	// 	if ok {
	// 		sbomImageList = append(sbomImageList, manifest.Image)
	// 	}
	// 	err = utils.SortImagesIndex(filepath.Join(buildPath, config.ImagesDir))
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// }

	if err := actions.Run(ctx, distroPath, onCreate.Defaults, onCreate.After, nil, nil); err != nil {
		return nil, fmt.Errorf("unable to run component before action: %w", err)
	}

	if !opts.SkipSBOM && d.IsSBOMAble() {
		l.Info("generating SBOM")
		l.Info("TODO generate sbom....")
	}

	d = recordDistroMetadata(d, opts.RegistryOverrides)

	b, err := goyaml.Marshal(d)
	if err != nil {
		return nil, err
	}
	err = os.WriteFile(filepath.Join(buildPath, config.ZarfDistroYaml), b, helpers.ReadWriteUser)
	if err != nil {
		return nil, err
	}

	return NewDistroLayout(buildPath, d), nil
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
			tmpDir, err := zutils.MakeTempDir(config.CommonOptions.TempDirectory)
			if err != nil {
				return err
			}
			defer func() {
				err = errors.Join(err, os.RemoveAll(tmpDir))
			}()
			compressedFile := filepath.Join(tmpDir, compressedFileName)

			// If the file is an archive, download it to the componentPath.Temp
			if err := zutils.DownloadToFile(ctx, file.Source, compressedFile); err != nil {
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
			if err := zutils.DownloadToFile(ctx, file.Source, dst); err != nil {
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
