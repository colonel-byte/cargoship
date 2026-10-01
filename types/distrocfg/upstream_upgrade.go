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

package distrocfg

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/colonel-byte/cargoship/api"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/utils"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// kubeadmPackagePath finds the staged kubeadm package file for host's package manager and
// architecture. kubeadm must install and run before kubelet/kubectl are touched, so it needs
// its own lookup rather than reusing the full-package-set install the shared upgrade phase
// already runs right after this step.
func kubeadmPackagePath(dis distro.ZarfDistro, host *cluster.ZarfHost, pkgManager string) (string, error) {
	rawArch, err := host.Arch()
	if err != nil {
		return "", fmt.Errorf("reading host architecture: %w", err)
	}
	arch := api.Arch(rawArch)

	for _, f := range dis.Spec.Config.OS.Files {
		if f.Selector.Package == pkgManager && f.Selector.MatchesArch(arch) && strings.HasPrefix(filepath.Base(f.Target), "kubeadm") {
			return f.Target, nil
		}
	}
	return "", fmt.Errorf("no staged kubeadm package found for %s/%s", pkgManager, arch)
}

// dnfVersionlockPresent reports whether the host's dnf answers the versionlock subcommand. --help
// is the probe because it neither reads repository metadata nor touches any existing lock.
//
// This mirrors the probe of the same name in pkg/phase, which cannot be imported here because
// pkg/phase imports this package. The two have to agree; see
// docs/agent/choice-unpinnable-hosts.md.
func dnfVersionlockPresent(host *cluster.ZarfHost) error {
	return host.SudoExec("dnf versionlock --help")
}

// unholdKubeadm releases the pin on kubeadm so the new package can install over it, on the hosts
// that can hold one at all.
//
// versionlock is a plugin a minimal RHEL-family install does not carry, and `dnf versionlock
// delete` on a host without it exits with "No such command: versionlock". Failing on that would
// break the upgrade on exactly the hosts phase 51 goes out of its way to keep installing, and it
// would break them harder, because this unhold runs before the install rather than after it. A
// host with no plugin has nothing locked, so there is nothing here to release. apt-mark ships in
// apt itself, so a Debian-family host can always release a hold and a failure there is a real
// error rather than absent tooling.
func unholdKubeadm(ctx context.Context, host *cluster.ZarfHost, pkgManager string) error {
	return unholdKubeadmUsing(ctx, host, pkgManager, dnfVersionlockPresent, func(h *cluster.ZarfHost, cmd string) error {
		return h.SudoExec(cmd)
	})
}

// unholdKubeadmUsing is unholdKubeadm with its two host commands injected, so a test can reach the
// missing-plugin path without a reachable host.
func unholdKubeadmUsing(ctx context.Context, host *cluster.ZarfHost, pkgManager string, present func(*cluster.ZarfHost) error, run func(*cluster.ZarfHost, string) error) error {
	if pkgManager == config.SelectorAPT {
		return run(host, "apt-mark unhold kubeadm")
	}
	if err := present(host); err != nil {
		// Debug rather than Warn: phase 51 warns about this host when it fails to re-pin the
		// full package set, and that warning is the one naming a real consequence.
		logger.From(ctx).Debug("this host has no dnf versionlock plugin, so kubeadm is not locked and there is nothing to release", "host", host, "error", err)
		return nil
	}
	return run(host, "dnf versionlock delete kubeadm")
}

// PreStartUpgrade runs kubeadm's own upgrade sequence between the shared upgrade phase's package
// install and service restart steps. kubeadm, unlike rke2/k3s, refuses to move the control plane
// forward on a plain kubelet restart -- it must install the new kubeadm binary and run its own
// upgrade command first, and the first controller runs a different command than every other node.
func (d *Upstream) PreStartUpgrade(ctx context.Context, host *cluster.ZarfHost, dis distro.ZarfDistro) error {
	pkgManager := config.SelectorAPT
	if !utils.FilterDebianLinux(host) {
		pkgManager = config.SelectorRPM
	}

	path, err := kubeadmPackagePath(dis, host, pkgManager)
	if err != nil {
		return err
	}

	if err := unholdKubeadm(ctx, host, pkgManager); err != nil {
		return fmt.Errorf("unholding kubeadm: %w", err)
	}
	if err := host.Configurer.InstallPackage(host, path); err != nil {
		return fmt.Errorf("installing new kubeadm: %w", err)
	}

	if host.Metadata.IsLeader {
		if _, err := host.SudoExecOutput("kubeadm upgrade plan"); err != nil {
			return fmt.Errorf("kubeadm upgrade plan: %w", err)
		}
		target := strings.TrimPrefix(dis.Spec.Version, "v")
		if _, err := host.SudoExecOutput(fmt.Sprintf("kubeadm upgrade apply v%s -y", target)); err != nil {
			return fmt.Errorf("kubeadm upgrade apply: %w", err)
		}
		return nil
	}

	if _, err := host.SudoExecOutput("kubeadm upgrade node"); err != nil {
		return fmt.Errorf("kubeadm upgrade node: %w", err)
	}
	return nil
}
