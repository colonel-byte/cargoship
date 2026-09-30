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

package phase

import (
	"context"
	"fmt"
	"strings"

	"github.com/colonel-byte/cargoship/api"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// debPackageName reads the package name recorded inside a staged .deb file, so pinning targets
// the name dpkg tracks rather than one guessed from the filename.
func debPackageName(h *cluster.ZarfHost, path string) (string, error) {
	return h.SudoExecOutput(fmt.Sprintf(`dpkg-deb --show --showformat="${Package}" %s`, path))
}

// rpmPackageName reads the package name recorded inside a staged .rpm file.
func rpmPackageName(h *cluster.ZarfHost, path string) (string, error) {
	return h.SudoExecOutput(fmt.Sprintf(`rpm -qp %s --queryformat "%%{NAME}"`, path))
}

// packageNames resolves the installed package name for each staged file, in order.
func packageNames(h *cluster.ZarfHost, files []v1alpha1.ZarfFile, nameOf func(*cluster.ZarfHost, string) (string, error)) ([]string, error) {
	names := make([]string, 0, len(files))
	for _, f := range files {
		name, err := nameOf(h, f.Target)
		if err != nil {
			return nil, fmt.Errorf("reading package name from %s: %w", f.Target, err)
		}
		names = append(names, name)
	}
	return names, nil
}

// holdAPTPackages pins packages at their installed version with apt-mark, so a host-level apt
// upgrade cannot drag them forward independently of cargoship. Unpinning them again, needed
// before an upgrade installs a newer set, belongs to the upgrade phase, not here. apt-mark ships
// in the apt package itself, so a Debian-family host that just installed these packages can
// always pin them and a failure here is a real error rather than a missing tool.
func holdAPTPackages(_ context.Context, h *cluster.ZarfHost, names []string) error {
	return h.SudoExec(fmt.Sprintf("apt-mark hold %s", strings.Join(names, " ")))
}

// dnfVersionlockPresent reports whether the host's dnf answers the versionlock subcommand. --help
// is the probe because it neither reads repository metadata nor touches any existing lock.
func dnfVersionlockPresent(h *cluster.ZarfHost) error {
	return h.SudoExec("dnf versionlock --help")
}

// addRPMVersionlock pins names at their installed version with dnf versionlock.
func addRPMVersionlock(h *cluster.ZarfHost, names []string) error {
	return h.SudoExec(fmt.Sprintf("dnf versionlock add %s", strings.Join(names, " ")))
}

// holdRPMPackages pins packages at their installed version with dnf versionlock, on the hosts that
// have it. Unlike apt-mark, versionlock is a plugin shipped in its own package that a minimal
// RHEL-family install does not carry and that cargoship cannot add over an air gap, so a host
// without it is left unpinned with a warning rather than failing an install that otherwise
// succeeded. See docs/agent/choice-unpinnable-hosts.md.
func holdRPMPackages(ctx context.Context, h *cluster.ZarfHost, names []string) error {
	return holdRPMPackagesUsing(ctx, h, names, dnfVersionlockPresent, addRPMVersionlock)
}

// holdRPMPackagesUsing is holdRPMPackages with its two host commands injected, so a test can reach
// the missing-plugin path without a reachable host.
func holdRPMPackagesUsing(ctx context.Context, h *cluster.ZarfHost, names []string, present func(*cluster.ZarfHost) error, add func(*cluster.ZarfHost, []string) error) error {
	if err := present(h); err != nil {
		logger.From(ctx).Warn("this host has no dnf versionlock plugin, engine packages are left unpinned against host-level updates", "host", h, "packages", names, "error", err)
		return nil
	}
	return add(h, names)
}

// installAndPinPackagesFor installs the packages built for a host's own architecture, then pins
// them at the installed version so a host-level package manager update cannot move the engine
// version out from under cargoship. nameOf reads the package name out of a staged file; hold runs
// the pinning command for the package manager in use.
//
// Both failures are fatal to the phase. A package name that cannot be read means the staged file
// is unreadable, whatever the package manager, and packageNames gives up on the first one it
// cannot read, so continuing would leave every package for the host unpinned. A hold that fails
// after that means the version guarantee did not take on a host whose tooling supports it.
// Deciding that a host cannot pin at all belongs to hold, which knows what its own tooling needs.
func (p *UploadFilesCommon) installAndPinPackagesFor(ctx context.Context, byArch map[api.Arch][]v1alpha1.ZarfFile, h *cluster.ZarfHost, nameOf func(*cluster.ZarfHost, string) (string, error), hold func(context.Context, *cluster.ZarfHost, []string) error) error {
	files, err := p.filesFor(byArch, h)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		logger.From(ctx).Warn("the package carries no engine packages for this host architecture, skipping the install", "host", h)
		return nil
	}

	if err := h.Configurer.InstallPackage(h, getPath(files)...); err != nil {
		return err
	}

	names, err := packageNames(h, files, nameOf)
	if err != nil {
		return err
	}
	return hold(ctx, h, names)
}
