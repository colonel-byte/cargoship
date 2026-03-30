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

package distro

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/colonel-byte/zarf-distro/src/pkg/utils"
	"github.com/zarf-dev/zarf/src/pkg/images"
	"github.com/zarf-dev/zarf/src/pkg/transform"
	zutils "github.com/zarf-dev/zarf/src/pkg/utils"
)

func (d *Distro) Create(ctx context.Context) error {
	if err := utils.ReadYAMLStrict(filepath.Join(d.cfg.CreateOpts.SourceDirectory, "distro.yaml"), &d.distro); err != nil {
		return err
	}

	if d.cfg.CreateOpts.Version != "" {
		d.distro.Metadata.Version = d.cfg.CreateOpts.Version
	}

	buildPath, err := zutils.MakeTempDir("/tmp")
	if err != nil {
		return err
	}

	componentImages := []transform.Image{}
	manifests := []images.ImageWithManifest{}
	for _, src := range d.distro.Spec.Distro.Config.Images {
		refInfo, err := transform.ParseImageRef(src)
		if err != nil {
			return fmt.Errorf("failed to create ref for image %s: %w", src, err)
		}
		if slices.Contains(componentImages, refInfo) {
			continue
		}
		componentImages = append(componentImages, refInfo)
	}

	if len(componentImages) > 0 {
		pullOpts := images.PullOptions{
			OCIConcurrency:        10,
			Arch:                  "amd64",
			RegistryOverrides:     []images.RegistryOverride{},
			CacheDirectory:        filepath.Join(d.cfg.CreateOpts.CachePath, ImagesDir),
			PlainHTTP:             false,
			InsecureSkipTLSVerify: false,
		}
		imageManifests, err := images.Pull(ctx, componentImages, filepath.Join(buildPath, ImagesDir), pullOpts)
		if err != nil {
			return err
		}
		manifests = append(manifests, imageManifests...)
	}

	for _, i := range manifests {
		fmt.Println(i.Image.Name)
	}

	// bytes, err := goyaml.Marshal(d.distro)
	// if err != nil {
	// 	return err
	// }
	// fmt.Println(string(bytes))

	return nil
}
