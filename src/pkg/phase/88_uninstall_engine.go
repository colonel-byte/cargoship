// Copyright 2023 k0sctl authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from k0sctl:
// https://github.com/k0sproject/k0sctl
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

package phase

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/types/distrocfg"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

var (
	aptPre   = regexp.MustCompile(`.*\.deb$`)
	rpmPre   = regexp.MustCompile(`.*\.rpm$`)
	pkgsType = []string{"rpm", "apt"}
)

// UninstallEngine state
type UninstallEngine struct {
	GenericPhase
	Distro           distrocfg.Distro
	WorkerConcurrent int
	hosts            cluster.ZarfHosts
}

// Title for the phase
func (p *UninstallEngine) Title() string {
	return "Uninstalling Engine"
}

// Explanation about the current phase, used for documentation generation
func (p *UninstallEngine) Explanation() string {
	return "Remove the rpm, apt, or binary files from all the hosts"
}

// Prepare the phase
func (p *UninstallEngine) Prepare(ctx context.Context, _ *cluster.ZarfCluster, _ *distro.ZarfDistro) error {
	p.hosts = p.manager.Config.Spec.Hosts
	logger.From(ctx).Debug("number of systems that need to be reset", "hosts", len(p.hosts))
	return nil
}

// Run the phase
func (p *UninstallEngine) Run(ctx context.Context) error {
	return p.batchedParallelWithMessage(
		ctx,
		"uninstalling engine files",
		p.hosts,
		p.WorkerConcurrent,
		p.stopService,
		p.uninstallNode,
	)
}

func (p *UninstallEngine) stopService(ctx context.Context, h *cluster.ZarfHost) error {
	if !h.IsController() {
		logger.From(ctx).Info("waiting for the service to stop", "service", p.Distro.GetWorkerService(), "host", h)
		return p.Distro.StopWorkerService(h)
	}
	logger.From(ctx).Info("waiting for the service to stop", "service", p.Distro.GetControllerService(), "host", h)
	return p.Distro.StopControllerService(h)
}

func (p *UninstallEngine) uninstallNode(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("uninstall", "node", h)
	packages := []string{}

	for _, pkg := range pkgsType {
		folder := filepath.Join(p.Distro.DataDirPath(), pkg)
		if h.FileExist(folder) {
			err := fs.WalkDir(h.Sudo().FS(), folder, func(_ string, d fs.DirEntry, _ error) error {
				if !d.IsDir() && rpmPre.MatchString(d.Name()) {
					cmd := fmt.Sprintf(`rpm -qp %s/%s --queryformat "%%{NAME}"`, folder, d.Name())
					output, err := h.Sudo().ExecOutput(cmd)
					if err != nil {
						logger.From(ctx).Warn("walking", "error", err, "output", output)
					}
					packages = append(packages, output)
				}
				if !d.IsDir() && aptPre.MatchString(d.Name()) {
					cmd := fmt.Sprintf(`dpkg-deb --show --showformat="${Package}" %s/%s`, folder, d.Name())
					output, err := h.Sudo().ExecOutput(cmd)
					if err != nil {
						logger.From(ctx).Warn("walking", "error", err, "output", output)
					}
					packages = append(packages, output)
				}
				return nil
			})
			if err != nil {
				logger.From(ctx).Warn("huh", "error", err)
			}
		}
	}

	slices.Sort(packages)
	pkg := slices.Compact(packages)

	if len(pkg) > 0 {
		if err := h.Configurer.UninstallPackage(h, pkg...); err != nil {
			logger.From(ctx).Warn("got", "error", err)
		}
	}

	if h.FileExist(p.Distro.DataDirPath()) {
		if err := h.Sudo().Exec(fmt.Sprintf("rm -rf %s", p.Distro.DataDirPath())); err != nil {
			logger.From(ctx).Warn("failed to remove engine data dir", "path", p.Distro.DataDirPath(), "error", err)
		}
	}

	confPath := p.Distro.ConfigPath()
	switch p.Distro.(type) {
	case *distrocfg.K3S, *distrocfg.RKE2:
		confPath = filepath.Dir(p.Distro.ConfigPath())
	}

	if h.FileExist(confPath) {
		if err := h.Sudo().Exec(fmt.Sprintf("rm -rf %s", confPath)); err != nil {
			logger.From(ctx).Warn("failed to remove engine config dir", "path", confPath, "error", err)
		}
	}
	// TODO: remove binaries......
	// need to sort and unique the results
	return nil
}
