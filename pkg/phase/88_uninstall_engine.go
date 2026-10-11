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
	"strings"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

var (
	debPre   = regexp.MustCompile(`.*\.deb$`)
	rpmPre   = regexp.MustCompile(`.*\.rpm$`)
	pkgsType = []string{"rpm", "deb"}
)

// UninstallEngine state
type UninstallEngine struct {
	GenericPhase
	Distro           distrocfg.Distro
	WorkerConcurrent string
	TargetHosts      []string
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
	p.hosts = filterTargetHosts(p.manager.Config.Spec.Hosts, p.TargetHosts)
	logger.From(ctx).Debug("number of systems that need to be reset", "hosts", len(p.hosts))
	return nil
}

// Run the phase
func (p *UninstallEngine) Run(ctx context.Context) error {
	return p.batchedParallelPerProfileWithMessage(
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

// preUninstallReset runs the distro's pre-package-removal teardown, when it implements one --
// kubeadm reset, unlike rke2/k3s where uninstalling the package is the whole story. Warns and
// continues on error, matching the rest of uninstallNode: a host that is unreachable or already
// reset should not block the rest of the uninstall.
func (p *UninstallEngine) preUninstallReset(ctx context.Context, h *cluster.ZarfHost) error {
	if r, ok := p.Distro.(distrocfg.PreUninstallResetter); ok {
		return r.PreUninstallReset(ctx, h)
	}
	return nil
}

func (p *UninstallEngine) uninstallNode(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("uninstall", "node", h)

	if err := p.preUninstallReset(ctx, h); err != nil {
		logger.From(ctx).Warn("failed to reset the host before removing packages", "error", err)
	}

	packages := []string{}

	if p.Distro != nil && p.Distro.PackageStagingDir() != "" {
		for _, pkg := range pkgsType {
			folder := filepath.Join(p.Distro.PackageStagingDir(), pkg)
			if pathExists(h, folder) {
				err := fs.WalkDir(h.Sudo().FS(), folder, func(_ string, d fs.DirEntry, _ error) error {
					if !d.IsDir() && rpmPre.MatchString(d.Name()) {
						cmd := fmt.Sprintf(`rpm -qp %s/%s --queryformat "%%{NAME}"`, folder, d.Name())
						output, err := h.Sudo().ExecOutput(cmd)
						if err != nil {
							logger.From(ctx).Warn("walking", "error", err, "output", output)
						}
						packages = append(packages, output)
					}
					if !d.IsDir() && debPre.MatchString(d.Name()) {
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
	}

	slices.Sort(packages)
	pkg := slices.Compact(packages)

	// If no packages were discovered from the staging folder (e.g. staging dir already cleaned up
	// or missing), query the system's package database for known engine packages matching the distro.
	if len(pkg) == 0 {
		pkg = p.detectInstalledEnginePackages(ctx, h)
	}

	if len(pkg) > 0 {
		if err := h.Configurer.UninstallPackage(h, pkg...); err != nil {
			logger.From(ctx).Warn("got", "error", err)
		}
	}

	for _, path := range p.Distro.CleanupPaths() {
		if !pathExists(h, path) {
			continue
		}
		if err := h.Sudo().Exec(fmt.Sprintf("rm -rf %s", path)); err != nil {
			logger.From(ctx).Warn("failed to remove engine path", "path", path, "error", err)
		}
	}
	p.cleanManagedDirs(ctx, h)
	p.cleanUploadManifest(ctx, h)

	return nil
}

// cleanManagedDirs cleans up managed directories and files tracked by the distro,
// including /etc/cargoship and any managed directories.
func (p *UninstallEngine) cleanManagedDirs(ctx context.Context, h *cluster.ZarfHost) {
	if p.Distro == nil {
		return
	}

	// Remove stale or all managed files in managed directories
	if err := distrocfg.RemoveStaleFiles(h, p.Distro.ManagedDirs(), nil); err != nil {
		logger.From(ctx).Warn("failed to remove managed files", "host", h, "error", err)
	}

	// Remove directories wholly owned by cargoship (non-globbed ManagedDirs)
	for _, md := range p.Distro.ManagedDirs() {
		if md.Glob != "" || md.Path == "" {
			continue
		}
		if pathExists(h, md.Path) {
			if err := h.Sudo().Exec(fmt.Sprintf("rm -rf %s", md.Path)); err != nil {
				logger.From(ctx).Warn("failed to remove managed dir", "host", h, "path", md.Path, "error", err)
			}
		}
	}

	// Always ensure /etc/cargoship state dir is cleaned if present
	if pathExists(h, distrocfg.StateDir) {
		if err := h.Sudo().Exec(fmt.Sprintf("rm -rf %s", distrocfg.StateDir)); err != nil {
			logger.From(ctx).Warn("failed to remove state dir", "host", h, "path", distrocfg.StateDir, "error", err)
		}
	}
}

// detectInstalledEnginePackages probes the host package manager for engine packages matching
// the distro when the staging directory holds no package files.
func (p *UninstallEngine) detectInstalledEnginePackages(ctx context.Context, h *cluster.ZarfHost) []string {
	var candidates []string
	binary := ""
	if p.Distro != nil {
		binary = p.Distro.BinaryName()
	}

	switch binary {
	case distrocfg.DistroRKE2:
		candidates = []string{
			"rke2-server",
			"rke2-agent",
			"rke2-common",
			"rke2-selinux",
		}
	case distrocfg.DistroK3S:
		candidates = []string{
			"k3s-server",
			"k3s-agent",
			"k3s-selinux",
		}
	case "kubectl", "kubeadm", distrocfg.DistroUpstream:
		candidates = []string{
			"kubelet",
			"kubeadm",
			"kubectl",
			"kubernetes-cni",
			"cri-tools",
		}
	default:
		return nil
	}

	var found []string
	for _, candidate := range candidates {
		// Test RPM query
		if out, err := h.Sudo().ExecOutput(fmt.Sprintf(`rpm -q --queryformat "%%{NAME}\n" %s`, candidate)); err == nil {
			for _, line := range strings.Split(out, "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !strings.Contains(line, "is not installed") {
					found = append(found, line)
				}
			}
			continue
		}
		// Test DPKG query
		if out, err := h.Sudo().ExecOutput(fmt.Sprintf(`dpkg-query -W -f='${Package}\n' %s`, candidate)); err == nil {
			for _, line := range strings.Split(out, "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !strings.Contains(line, "no packages found") {
					found = append(found, line)
				}
			}
		}
	}

	slices.Sort(found)
	found = slices.Compact(found)
	if len(found) > 0 {
		logger.From(ctx).Info("discovered installed engine packages from package manager", "host", h, "packages", found)
	}
	return found
}

// pathExists reports whether anything is at path on the host, directory or file.
//
// Not ZarfHost.FileExist, which is `test -f` and therefore false for a directory. Every path an
// uninstall removes is a directory -- the engine's data directory, its config directory,
// /etc/cargoship -- so guarding their removal on FileExist skipped all of them, and skipped
// them silently: nothing was attempted, so nothing failed and nothing was logged. A reset left
// 2.3G in /var/lib/rancher/rke2 and a config.yaml naming the old cluster, while reporting
// success and promising in its own help text to remove "the engine and the data it wrote".
func pathExists(h *cluster.ZarfHost, path string) bool {
	if path == "" {
		return false
	}
	_, err := h.Stat(path)
	return err == nil
}
