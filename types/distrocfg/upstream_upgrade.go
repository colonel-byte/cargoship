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

// PreStartUpgrade runs kubeadm's own upgrade sequence between the shared upgrade phase's package
// install and service restart steps. kubeadm, unlike rke2/k3s, refuses to move the control plane
// forward on a plain kubelet restart -- it must install the new kubeadm binary and run its own
// upgrade command first, and the first controller runs a different command than every other node.
func (d *Upstream) PreStartUpgrade(_ context.Context, host *cluster.ZarfHost, dis distro.ZarfDistro) error {
	pkgManager := config.SelectorAPT
	if !utils.FilterDebianLinux(host) {
		pkgManager = config.SelectorRPM
	}

	path, err := kubeadmPackagePath(dis, host, pkgManager)
	if err != nil {
		return err
	}

	unholdKubeadm := "dnf versionlock delete kubeadm"
	if pkgManager == config.SelectorAPT {
		unholdKubeadm = "apt-mark unhold kubeadm"
	}
	if err := host.SudoExec(unholdKubeadm); err != nil {
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
