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
	"errors"
	"fmt"

	"github.com/colonel-byte/mare/src/pkg/packager/layout"
	"github.com/colonel-byte/mare/src/pkg/packager/load"
	"github.com/zarf-dev/zarf/src/pkg/images"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/types"
)

// CreateOptions are the optional parameters to create
type CreateOptions struct {
	RegistryOverrides []images.RegistryOverride
	OCIConcurrency    int
	CachePath         string
	IsInteractive     bool
	SkipSBOM          bool
	types.RemoteOptions
}

func Create(ctx context.Context, distroPath string, output string, opts CreateOptions) (_ string, err error) {
	loadOpts := load.DefinitionOptions{
		CachePath:     opts.CachePath,
		RemoteOptions: opts.RemoteOptions,
	}
	distro, err := load.DistroDefinition(ctx, distroPath, loadOpts)
	if err != nil {
		return "", err
	}

	disPath, err := layout.ResolveDistroPath(distroPath)
	if err != nil {
		return "", fmt.Errorf("unable to access package path %q: %w", distroPath, err)
	}

	assembleOpt := layout.AssembleOptions{
		RegistryOverrides: opts.RegistryOverrides,
		RemoteOptions:     opts.RemoteOptions,
		OCIConcurrency:    opts.OCIConcurrency,
		CachePath:         opts.CachePath,
		// Don't have sbom logic yet....
		SkipSBOM: true,
	}

	logger.From(ctx).Debug("assembling distro", "disPath.BaseDir", disPath.BaseDir)
	distroLayout, err := layout.AssembleDistro(ctx, distro, disPath.BaseDir, assembleOpt)
	if err != nil {
		return "", err
	}
	defer func() {
		err = errors.Join(err, distroLayout.Cleanup())
	}()

	var distroLocation string
	distroLocation, err = distroLayout.Archive(ctx, output, 0)
	if err != nil {
		return "", err
	}

	return distroLocation, nil
}
