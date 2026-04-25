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

// Package layout is used to defining the distro package files
package layout

import (
	"context"
	"os"
	"path/filepath"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/internal/distrocfg"
	"github.com/colonel-byte/cargoship/src/types"
	"github.com/zarf-dev/zarf/src/pkg/archive"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	zutils "github.com/zarf-dev/zarf/src/pkg/utils"
)

// Distro struct
type Distro struct {
	cfg    *types.DistroConfig
	distro distro.ZarfDistro
	tmp    string
}

// DistroLayout struct
type DistroLayout struct {
	dirPath string
	Distro  distro.ZarfDistro
}

// DistroLayoutOptions struct
type DistroLayoutOptions struct{}

// New creates a new Distro object
func New(cfg *types.DistroConfig) (*Distro, error) {
	dis := Distro{
		cfg:    cfg,
		distro: distro.ZarfDistro{},
		tmp:    "/tmp",
	}

	return &dis, nil
}

// LoadFromTar unpacks the given archive (any compress/format) and loads it.
func LoadFromTar(ctx context.Context, tarPath string, opts DistroLayoutOptions) (*DistroLayout, error) {
	dirPath, err := zutils.MakeTempDir(config.CommonOptions.TempDirectory)
	if err != nil {
		return nil, err
	}
	// Decompress the archive
	err = archive.Decompress(ctx, tarPath, dirPath, archive.DecompressOpts{})
	if err != nil {
		return nil, err
	}

	// 3) Delegate to the existing LoadFromDir
	return LoadFromDir(ctx, dirPath, opts)
}

// LoadFromDir loads and validates a package from the given directory path.
func LoadFromDir(ctx context.Context, dirPath string, _ DistroLayoutOptions) (*DistroLayout, error) {
	b, err := os.ReadFile(filepath.Join(dirPath, config.ZarfDistroYaml))
	if err != nil {
		return nil, err
	}
	dis, err := distrocfg.Parse(ctx, b)
	if err != nil {
		return nil, err
	}
	disLayout := &DistroLayout{
		dirPath: dirPath,
		Distro:  dis,
	}

	logger.From(ctx).Debug(dis.Metadata.Name)

	return disLayout, nil
}
