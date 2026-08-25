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

// Package distro is used for creating/deploying distro package
package distro

import (
	"context"
	"errors"
	"fmt"

	"github.com/colonel-byte/cargoship/src/pkg/packager/assemble"
	"github.com/colonel-byte/cargoship/src/pkg/packager/layout"
	"github.com/colonel-byte/cargoship/src/pkg/packager/load"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/types"
)

// CreateOptions are the optional parameters to create
type CreateOptions struct {
	OCIConcurrency int
	CachePath      string
	IsInteractive  bool
	SkipSBOM       bool
	// Reproducible pins Build.Timestamp to config.InitCommit instead of the
	// current time, and is recorded on Build.Reproducible, so identical package
	// inputs produce byte-identical output.
	Reproducible bool
	// SigningKeyPath and SigningKeyPassword sign the package as part of creation
	// when set. Empty values are a no-op -- see DistroLayout.SignPackage.
	SigningKeyPath     string
	SigningKeyPassword string
	types.RemoteOptions
}

// Create used to create a distro package
func Create(ctx context.Context, distroPath string, output string, opts CreateOptions) (string, error) {
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

	assembleOpt := assemble.AssembleOptions{
		RemoteOptions:      opts.RemoteOptions,
		OCIConcurrency:     opts.OCIConcurrency,
		CachePath:          opts.CachePath,
		Reproducible:       opts.Reproducible,
		SigningKeyPath:     opts.SigningKeyPath,
		SigningKeyPassword: opts.SigningKeyPassword,
		// Don't have sbom logic yet....
		SkipSBOM: true,
	}

	logger.From(ctx).Debug("assembling distro", "baseDir", disPath.BaseDir)
	distroLayout, err := assemble.AssembleDistro(ctx, distro, disPath.BaseDir, assembleOpt)
	if err != nil {
		return "", err
	}
	defer func() {
		err = errors.Join(err, distroLayout.Cleanup())
	}()

	return distroLayout.Archive(ctx, output, 0)
}
