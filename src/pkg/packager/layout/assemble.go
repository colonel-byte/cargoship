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

func AssembleDistro(ctx context.Context, d v1alpha1.ZarfDistroPackage, packagePath string, opts AssembleOptions) (*packager.DistroLayout, error) {
	l := logger.From(ctx)
	l.Info("assembling distro", "path", packagePath)

	// TODO(a1994sc): remove hard coded temp directory
	buildPath, err := zutils.MakeTempDir("/tmp")
	if err != nil {
		return nil, err
	}

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
		imageManifests, err := images.Pull(ctx, componentImages, filepath.Join(buildPath, config.ImagesDir), pullOpts)
		if err != nil {
			return nil, err
		}
		manifests = append(manifests, imageManifests...)
	}
	return nil, nil
}
