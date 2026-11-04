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
	"os"
	"path/filepath"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/colonel-byte/cargoship/pkg/contentstore"
	"github.com/colonel-byte/cargoship/pkg/fileoverride"
	"github.com/colonel-byte/cargoship/pkg/haulerstore"
	"github.com/colonel-byte/cargoship/pkg/images"
	"github.com/colonel-byte/cargoship/pkg/packager/assemble"
	"github.com/colonel-byte/cargoship/pkg/packager/layout"
	"github.com/colonel-byte/cargoship/pkg/packager/load"
	"github.com/colonel-byte/cargoship/pkg/zarfpackage"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/types"
)

// CreateOptions are the optional parameters to create
type CreateOptions struct {
	OCIConcurrency int
	CachePath      string
	IsInteractive  bool
	// Architecture narrows a package that targets several architectures down to a single one.
	// A package that targets one architecture ignores it.
	Architecture string
	// Reproducible pins Build.Timestamp to config.Timestamp instead of the
	// current time, and is recorded on Build.Reproducible, so identical package
	// inputs produce byte-identical output.
	Reproducible      bool
	RegistryOverrides []images.RegistryOverride
	// FileOverrides redirect the file downloads a distro definition declares to an internal
	// mirror or to a directory of pre-staged assets.
	FileOverrides []fileoverride.Override
	// ContentStore is the path to a local content bundle that images and files are resolved
	// from before the network: a hauler store directory or archive, or a built zarf package.
	// The format is read from the bundle, not from the path. Empty means no store.
	ContentStore string
	// SigningKeyPath and SigningKeyPassword sign the package as part of creation
	// when set. Empty values are a no-op -- see DistroLayout.SignPackage.
	SigningKeyPath     string
	SigningKeyPassword string
	// Tag overrides the package metadata version.
	Tag string
	types.RemoteOptions
}

// Create used to create a distro package
func Create(ctx context.Context, distroPath string, output string, opts CreateOptions) (string, error) {
	loadOpts := load.DefinitionOptions{
		CachePath:     opts.CachePath,
		Architecture:  opts.Architecture,
		RemoteOptions: opts.RemoteOptions,
	}
	distro, err := load.DistroDefinition(ctx, distroPath, loadOpts)
	if err != nil {
		return "", err
	}
	if opts.Tag != "" {
		distro.Metadata.Version = opts.Tag
	}

	disPath, err := layout.ResolveDistroPath(distroPath)
	if err != nil {
		return "", fmt.Errorf("unable to access package path %q: %w", distroPath, err)
	}

	// The store is opened once for the whole build and closed with it, because opening an
	// archive unpacks it and every file and image staged below reads from that one copy.
	var store assemble.ContentStore
	if opts.ContentStore != "" {
		opened, closeStore, err := openContentStore(ctx, opts.ContentStore)
		if err != nil {
			return "", err
		}
		store = opened
		// Create's results are unnamed, so a deferred assignment to err would be discarded.
		// Closing only removes the directory an archive was unpacked into, so a failure is
		// reported rather than returned.
		defer func() {
			if cerr := closeStore(); cerr != nil {
				logger.From(ctx).Warn("failed to remove the unpacked content store", "error", cerr)
			}
		}()
		logger.From(ctx).Info("resolving package content from a local content store", "store", store.Describe())
	}

	assembleOpt := assemble.AssembleOptions{
		RemoteOptions:      opts.RemoteOptions,
		OCIConcurrency:     opts.OCIConcurrency,
		CachePath:          opts.CachePath,
		Reproducible:       opts.Reproducible,
		RegistryOverrides:  opts.RegistryOverrides,
		FileOverrides:      opts.FileOverrides,
		ContentStore:       store,
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

// Compile-time proof that both readers satisfy the one interface a build resolves content
// against. This is the only place that matters: pkg/packager/assemble declares the interface and
// imports neither reader, and pkg/images knows about neither.
var (
	_ assemble.ContentStore = (*haulerstore.Store)(nil)
	_ assemble.ContentStore = (*zarfpackage.Package)(nil)
)

// openContentStore unpacks the content bundle at path once and hands the unpacked tree to the
// reader whose layout it matches, returning a function that removes anything the unpack created.
//
// The format is decided by what the bundle holds rather than by what it is called: a zarf package
// is named by convention, not by rule, and an operator who renamed one should still get a build
// rather than a complaint. A single unpack is also the reason the decision is made here instead
// of inside the readers -- sniffing an archive without unpacking it means a second pass over the
// whole compressed stream, which on a package carrying a fleet's images is not free.
func openContentStore(ctx context.Context, path string) (assemble.ContentStore, func() error, error) {
	dir, tempDir, err := contentstore.Unpack(ctx, path)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() error {
		if tempDir == "" {
			return nil
		}
		return os.RemoveAll(tempDir)
	}

	// zarf.yaml first: a zarf package also carries an images/index.json, so testing for the
	// hauler layout first would read every zarf package as a haul with no content.
	switch {
	case exists(filepath.Join(dir, zarfDefinition)):
		store, err := zarfpackage.OpenDir(ctx, dir, path)
		if err != nil {
			return nil, nil, errors.Join(err, cleanup())
		}
		return store, cleanup, nil
	case exists(filepath.Join(dir, ocispec.ImageIndexFile)):
		store, err := haulerstore.OpenDir(ctx, dir, path)
		if err != nil {
			return nil, nil, errors.Join(err, cleanup())
		}
		return store, cleanup, nil
	}

	return nil, nil, errors.Join(
		fmt.Errorf("%s is not a content store cargoship can read: a zarf package has %s at its root, and a hauler store has %s", path, zarfDefinition, ocispec.ImageIndexFile),
		cleanup(),
	)
}

// zarfDefinition is the file whose presence at a bundle's root identifies a zarf package.
const zarfDefinition = "zarf.yaml"

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
