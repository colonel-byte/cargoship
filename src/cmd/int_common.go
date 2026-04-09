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

package cmd

import (
	"context"
	"path/filepath"

	"github.com/colonel-byte/zarf-distro/src/config"
	"github.com/colonel-byte/zarf-distro/src/pkg/packager"
	"github.com/colonel-byte/zarf-distro/src/pkg/packager/load"
	"github.com/colonel-byte/zarf-distro/src/pkg/phase"
	"github.com/colonel-byte/zarf-distro/src/types/distro"
	"github.com/colonel-byte/zarf-distro/src/types/distro/registry"
	zconfig "github.com/zarf-dev/zarf/src/config"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type InstallCommon struct {
	config      string
	concurrency int
	confirm     bool
	logLevel    string
	LogFormat   string
}

func Distro(s string) (distro.Distro, error) {
	ds, err := registry.GetDistroModuleBuilder(s)
	if err != nil {
		return nil, err
	}
	d := ds().(distro.Distro)
	return d, nil
}

func initManager(ctx context.Context, distroPath string, opt InstallCommon) (*phase.Manager, error) {
	path, err := filepath.Abs(opt.config)
	if err != nil {
		return nil, err
	}

	opt.config = path

	cluster, err := load.ClusterDefinition(ctx, opt.config, load.ClusterOptions{})
	if err != nil {
		return nil, err
	}

	logger.From(ctx).Info("using cluster file", "location", opt.config)

	loadOpts := packager.LoadOptions{
		CachePath:    config.CommonOptions.CachePath,
		Architecture: zconfig.CLIArch,
		Output:       config.CommonOptions.TempDirectory,
	}

	distroLayout, err := packager.LoadDistro(ctx, distroPath, loadOpts)
	if err != nil {
		return nil, err
	}

	logger.From(ctx).Debug("distro information", "temp", distroLayout.DirPath(), "build", distroLayout.Distro.Build.Timestamp)

	return &phase.Manager{
		Config:            &cluster,
		Distro:            &distroLayout.Distro,
		TempDirectory:     distroLayout.DirPath(),
		Concurrency:       opt.concurrency,
		ConcurrentUploads: opt.concurrency,
		DryRun:            false,
		DistroCfg: phase.ManagerDistroConfig{
			BinaryDir: "/usr/local/bin",
			Binary:    "rke2",
		},
	}, nil
}
