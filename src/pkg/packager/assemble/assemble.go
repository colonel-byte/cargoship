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
	"maps"
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
	"github.com/colonel-byte/cargoship/src/pkg/engineconfig/gen"
	"github.com/colonel-byte/cargoship/src/pkg/helpers"
	"github.com/colonel-byte/cargoship/src/pkg/images"
	"github.com/colonel-byte/cargoship/src/pkg/packager/layout"
	"github.com/colonel-byte/cargoship/src/pkg/utils"
	goyaml "github.com/goccy/go-yaml"
	"github.com/k0sproject/dig"
	zlang "github.com/zarf-dev/zarf/src/config/lang"
	"github.com/zarf-dev/zarf/src/pkg/archive"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/packager/actions"
	"github.com/zarf-dev/zarf/src/pkg/signing"
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
	// Reproducible pins Build.Timestamp to config.Timestamp instead of the
	// current time, and is recorded on Build.Reproducible, so identical package
	// inputs produce byte-identical output.
	Reproducible bool
	// SigningKeyPath and SigningKeyPassword sign the package as part of assembly when
	// set. Empty values are a no-op -- see DistroLayout.SignPackage.
	SigningKeyPath     string
	SigningKeyPassword string
	types.RemoteOptions
}

// logUnknownEngineConfig logs engine config keys the distro version being packaged does not
// recognize, so a typo is visible here rather than only on every node at install time.
//
// This only ever logs, at debug. The generated schemas cover the versions whose source has been
// pulled into this build, which is not necessarily the version a package targets, and a package
// that carries a key this binary has never heard of is still a package worth building. Install
// time keeps the authoritative check, where the key is narrowed to the role the node actually
// plays and an unrecognized one is dropped from the config it writes.
func logUnknownEngineConfig(ctx context.Context, d distro.ZarfDistro) {
	cfg := engineConfigKeys(d.Spec.Config.Engine[config.EngineConfig])
	if len(cfg) == 0 {
		return
	}

	l := logger.From(ctx)
	entry, ok := gen.Lookup(d.Spec.Type, d.Spec.Version)
	if !ok {
		l.Debug("no generated engine config schema for this distro/version, skipping config key validation",
			"distro", d.Spec.Type, "version", d.Spec.Version)
		return
	}

	// A package installs controllers and workers alike, so a key either role accepts belongs
	// here.
	valid := gen.Keys(entry.Server)
	maps.Copy(valid, gen.Keys(entry.Agent))

	for _, k := range slices.Sorted(maps.Keys(cfg)) {
		if _, ok := valid[k]; !ok {
			l.Debug("engine config key not recognized for this distro/version, it will be dropped at install time",
				"distro", d.Spec.Type, "version", d.Spec.Version, "key", k)
		}
	}
}

// engineConfigKeys is the engine config block, whichever map shape it was decoded into.
func engineConfigKeys(v any) map[string]any {
	switch m := v.(type) {
	case dig.Mapping:
		return m
	case map[string]any:
		return m
	default:
		return nil
	}
}

// AssembleDistro creates the actual tarballs
func AssembleDistro(ctx context.Context, d distro.ZarfDistro, distroPath string, opts AssembleOptions) (*layout.DistroLayout, error) {
	l := logger.From(ctx)
	l.Info("assembling distro", "path", distroPath)
	logUnknownEngineConfig(ctx, d)

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
		arches := d.Metadata.Arches()
		for _, arch := range arches {
			pullOpts := images.PullOptions{
				OCIConcurrency:        opts.OCIConcurrency,
				Arch:                  string(arch),
				RegistryOverrides:     opts.RegistryOverrides,
				CacheDirectory:        filepath.Join(opts.CachePath, config.ImagesDir),
				PlainHTTP:             opts.PlainHTTP,
				InsecureSkipTLSVerify: opts.InsecureSkipTLSVerify,
			}
			imagesPath := imageDirForArch(buildPath, arch, len(arches))
			l.Info("pulling images too", "path", imagesPath, "architecture", arch)
			_, err := images.Pull(ctx, componentImages, imagesPath, pullOpts)
			if err != nil {
				return nil, err
			}
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
	err = os.WriteFile(checksumPath, []byte(checksumContent), helpers.ReadAllWriteUser)
	if err != nil {
		return nil, err
	}
	d.Metadata.AggregateChecksum = checksumSha

	d = recordDistroMetadata(d, opts)

	b, err := goyaml.Marshal(d)
	if err != nil {
		return nil, err
	}
	err = os.WriteFile(filepath.Join(buildPath, config.DistroYAML), b, helpers.ReadAllWriteUser)
	if err != nil {
		return nil, err
	}

	distroLayout := layout.NewDistroLayout(buildPath, d)

	signOpts := signing.DefaultSignBlobOptions()
	signOpts.Key = opts.SigningKeyPath
	signOpts.Password = opts.SigningKeyPassword
	if err := distroLayout.SignPackage(ctx, signOpts); err != nil {
		return nil, err
	}

	return distroLayout, nil
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
		err := os.Chmod(dst, helpers.ReadExecuteAllWriteUser)
		if err != nil {
			return err
		}
	} else {
		err := os.Chmod(dst, helpers.ReadAllWriteUser)
		if err != nil {
			return err
		}
	}
	return nil
}

// buildTimestamp returns the timestamp to record as Build.Timestamp. When
// reproducible is true (--reproducible), it's pinned to config.Timestamp instead
// of the current time, so two builds from identical inputs produce byte-identical
// output. Mirrors "flux push artifact --reproducible".
func buildTimestamp(reproducible bool) time.Time {
	if reproducible {
		return config.Timestamp
	}
	return time.Now()
}

// imageDirForArch returns the directory an architecture's images belong in. A package targeting a
// single architecture keeps the flat images directory it has always used. A package targeting
// several needs one directory per architecture, because the OCI store tags its contents by image
// reference, so two platforms of the same reference in one layout would leave the tag pointing at
// whichever pull finished last.
func imageDirForArch(buildPath string, arch api.Arch, archCount int) string {
	if archCount < 2 {
		return filepath.Join(buildPath, config.ImagesDir)
	}

	return filepath.Join(buildPath, config.ImagesDir, string(arch))
}

func recordDistroMetadata(distro distro.ZarfDistro, opts AssembleOptions) distro.ZarfDistro {
	arches := distro.Metadata.Arches()
	distro.Build.Architectures = arches
	// The scalar stays populated for a single architecture package so that readers which only know
	// about it, such as an older cargoship, still see the architecture they expect.
	if len(arches) == 1 {
		distro.Build.Architecture = arches[0]
	}
	distro.Build.Timestamp = buildTimestamp(opts.Reproducible).Format(api.BuildTimestampFormat)
	distro.Build.Version = distro.Metadata.Version
	distro.Build.Reproducible = opts.Reproducible

	overrides := make(map[string]string, len(opts.RegistryOverrides))
	for i := range opts.RegistryOverrides {
		overrides[opts.RegistryOverrides[i].Source] = opts.RegistryOverrides[i].Override
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
