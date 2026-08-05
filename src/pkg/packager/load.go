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

// Package packager for interacting with a packages distro config
package packager

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/pkg/packager/layout"
	"github.com/colonel-byte/cargoship/src/pkg/utils"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/signing"
	"github.com/zarf-dev/zarf/src/types"
)

// LoadOptions are the options for LoadDistro.
type LoadOptions struct {
	Shasum       string
	Architecture string
	Output       string
	// number of layers to pull in parallel
	OCIConcurrency int
	// CachePath is used to cache layers from OCI package pulls
	CachePath         string
	VerifyBlobOptions *signing.VerifyBlobOptions
	// Only applicable to OCI + HTTP
	types.RemoteOptions
	// VerificationStrategy for explicit definition
	layout.VerificationStrategy
}

// LoadDistro fetches, verifies, and loads a Zarf package from the specified source.
func LoadDistro(ctx context.Context, source string, opts LoadOptions) (*layout.DistroLayout, error) {
	if source == "" {
		return nil, fmt.Errorf("must provide a package source")
	}

	srcType, err := utils.IdentifySource(source)
	if err != nil {
		return nil, err
	}

	// Prepare a temp workspace
	tmpDir, err := utils.MakeTempDir(config.CommonOptions.TempDirectory)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, os.RemoveAll(tmpDir))
	}()

	tmpPath := filepath.Join(tmpDir, "data.tar.zst") //nolint:ineffassign,staticcheck
	switch srcType {
	case "oci":
		ociOpts := pullOCIOptions{
			Source:               source,
			VerifyBlobOptions:    opts.VerifyBlobOptions,
			VerificationStrategy: opts.VerificationStrategy,
			Shasum:               opts.Shasum,
			Architecture:         config.GetArch(opts.Architecture),
			OCIConcurrency:       opts.OCIConcurrency,
			RemoteOptions:        opts.RemoteOptions,
			CachePath:            opts.CachePath,
		}

		disLayout, err := pullOCI(ctx, ociOpts)
		if err != nil {
			return nil, err
		}
		// OCI is a special case since it doesn't create a tar unless the tar file is output
		if opts.Output != "" {
			_, err := disLayout.Archive(ctx, opts.Output, 0)
			if err != nil {
				return nil, err
			}
		}
		return disLayout, nil
	case "http", "https":
		tmpPath, err = pullHTTP(ctx, source, tmpDir, opts.Shasum, opts.InsecureSkipTLSVerify)
		if err != nil {
			return nil, err
		}
	case "tarball":
		tmpPath = source
	default:
		err := fmt.Errorf("cannot fetch or locate tarball for unsupported source type %s", srcType)
		return nil, err
	}
	logger.From(ctx).Debug(tmpPath)
	distroLayout, err := layout.LoadFromTar(ctx, tmpPath, layout.DistroLayoutOptions{})
	if err != nil {
		return nil, err
	}

	if opts.Output != "" {
		filename, err := distroLayout.FileName()
		if err != nil {
			return nil, err
		}
		tarPath := filepath.Join(opts.Output, filename)
		err = os.Remove(tarPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		dstFile, err := os.Create(tarPath)
		if err != nil {
			return nil, err
		}
		defer func() {
			err = errors.Join(err, dstFile.Close())
		}()
		srcFile, err := os.Open(tmpPath)
		if err != nil {
			return nil, err
		}
		defer func() {
			err = errors.Join(err, srcFile.Close())
		}()
		_, err = io.Copy(dstFile, srcFile)
		if err != nil {
			return nil, err
		}
	}

	return distroLayout, nil
}
