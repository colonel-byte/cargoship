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
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"github.com/colonel-byte/zarf-distro/src/api"
	"github.com/colonel-byte/zarf-distro/src/api/v1alpha1"
	"github.com/colonel-byte/zarf-distro/src/config"
	"github.com/colonel-byte/zarf-distro/src/pkg/packager"
	"github.com/zarf-dev/zarf/src/pkg/images"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/transform"
	zutils "github.com/zarf-dev/zarf/src/pkg/utils"
	"github.com/zarf-dev/zarf/src/types"
)

type AssembleOptions struct {
	RegistryOverrides []images.RegistryOverride
	OCIConcurrency    int
	CachePath         string
	types.RemoteOptions
}

func AssembleDistro(ctx context.Context, d v1alpha1.ZarfDistroPackage, distroPath string, opts AssembleOptions) (*packager.DistroLayout, error) {
	l := logger.From(ctx)
	l.Info("assembling distro", "path", distroPath)

	buildPath, err := zutils.MakeTempDir(config.CommonOptions.TempDirectory)
	if err != nil {
		return nil, err
	}
	l.Info("using build path", "buildPath", buildPath)

	componentImages := []transform.Image{}
	manifests := []images.ImageWithManifest{}
	for _, src := range d.Spec.Distro.Config.Images {
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

	d = recordDistroMetadata(d, opts.RegistryOverrides)

	return packager.NewDistroLayout(buildPath, d), nil
}

func recordDistroMetadata(distro v1alpha1.ZarfDistroPackage, registryOverrides []images.RegistryOverride) v1alpha1.ZarfDistroPackage {
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
