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

package packager

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/colonel-byte/zarf-distro/src/config"
	"github.com/colonel-byte/zarf-distro/src/pkg/packager/layout"
	"github.com/colonel-byte/zarf-distro/src/pkg/utils"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	zutils "github.com/zarf-dev/zarf/src/pkg/utils"
	"github.com/zarf-dev/zarf/src/types"
)

// LoadOptions are the options for LoadDistro.
type LoadOptions struct {
	Shasum         string
	Architecture   string
	Output         string
	OCIConcurrency int
	CachePath      string
	types.RemoteOptions
}

// LoadPackage fetches, verifies, and loads a Zarf package from the specified source.
func LoadDistro(ctx context.Context, source string, opts LoadOptions) (*layout.DistroLayout, error) {
	if source == "" {
		return nil, fmt.Errorf("must provide a package source")
	}

	srcType, err := utils.IdentifySource(source)
	if err != nil {
		return nil, err
	}

	// Prepare a temp workspace
	tmpDir, err := zutils.MakeTempDir(config.CommonOptions.TempDirectory)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, os.RemoveAll(tmpDir))
	}()

	tmpPath := filepath.Join(tmpDir, "data.tar.zst")
	switch srcType {
	// TODO borrow from https://github.com/zarf-dev/zarf/blob/2233efff3e4aeb86a604ec7c3fd67f6caf4116e5/src/pkg/packager/load.go#L78
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
