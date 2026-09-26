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
	"regexp"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/types/distrocfg/registry"
)

const (
	// DistroUpstream id
	DistroUpstream = "upstream"

	// upstreamKubeconfig is the fixed path kubeadm writes the cluster admin kubeconfig to.
	upstreamKubeconfig = "/etc/kubernetes/admin.conf"

	// upstreamServiceCIDR is kubeadm's default service subnet, used when the engine config does
	// not set one. Unlike the pod subnet, kubeadm always has this default.
	upstreamServiceCIDR = "10.96.0.0/12"

	// keyPodSubnet and keyServiceSubnet are the engine config keys upstream reads its cluster
	// CIDRs from. There is no cross-engine standard for these names, so they are upstream's own.
	keyPodSubnet     = "podSubnet"
	keyServiceSubnet = "serviceSubnet"
)

// upstreamVersionRegex matches the version kubelet --version prints, e.g. "Kubernetes v1.35.3".
// Unlike rke2/k3s, upstream kubelet carries no engine suffix.
var upstreamVersionRegex = regexp.MustCompile(`v?[0-9]+\.[0-9]+\.[0-9]+`)

// Upstream distro struct. It targets a kubeadm-bootstrapped cluster: plain kubelet/kubeadm/
// kubectl packages and upstream containerd, none of which share rke2/k3s's single config.yaml or
// wharfie registries.yaml, so it implements Distro directly rather than embedding RancherCommon.
type Upstream struct {
	Common
}

var _ Distro = (*Upstream)(nil)

func init() {
	registry.RegisterDistroModule(
		func(dis string) bool {
			return dis == DistroUpstream
		},
		func() any {
			return &Upstream{
				Common{
					ID:                DistroUpstream,
					BinaryDir:         "/usr/bin",
					Binary:            "kubectl",
					Config:            "/etc/kubernetes",
					Data:              "/var/lib/kubelet",
					PackageDir:        "/var/lib/kubernetes",
					ServiceController: "kubelet",
					ServiceWorker:     "kubelet",
				},
			}
		},
	)
}

// AdminCredentials returns the cluster CA certificate and the admin client key pair, read out of
// the admin kubeconfig kubeadm writes on a controller host.
func (d *Upstream) AdminCredentials(host *cluster.ZarfHost, dataDir string) (AdminCredentials, error) {
	return adminCredentials(host, d.KubeconfigPath(host, dataDir))
}

// KubeconfigPath returns the path to the admin config kubeadm writes. Unlike rke2/k3s, this path
// does not vary by host or data directory.
func (d *Upstream) KubeconfigPath(_ *cluster.ZarfHost, _ string) string {
	return upstreamKubeconfig
}

// KubectlCmdf returns a string that can be executed to interact with the kubernetes cluster.
// kubectl is a plain host binary here, not reached through an engine wrapper.
func (d *Upstream) KubectlCmdf(host *cluster.ZarfHost, dataDir string, s string, args ...any) string {
	return fmt.Sprintf(`env "KUBECONFIG=%s" kubectl %s`, d.KubeconfigPath(host, dataDir), fmt.Sprintf(s, args...))
}

// DistroCmdf returns a string that can be used to execute a command directly. Upstream has no
// single engine binary to wrap a command through -- kubeadm, kubelet, and kubectl are three
// separate packages -- so the template is formatted as-is.
func (d *Upstream) DistroCmdf(template string, args ...any) string {
	return fmt.Sprintf(template, args...)
}

// RunningVersion returns the version of kubelet running on the host, if the engine is not
// running it throws an "ErrVersionNotDetected" error
func (d *Upstream) RunningVersion(host *cluster.ZarfHost) (string, error) {
	bin, err := host.FS().LookPath("kubelet")
	if err != nil {
		return "", ErrVersionNotDetected
	}
	out, err := host.ExecOutput(fmt.Sprintf(`%s --version`, bin))
	if err != nil {
		return "", ErrVersionNotDetected
	}
	match := upstreamVersionRegex.FindString(out)
	if match == "" {
		return "", ErrVersionNotDetected
	}
	return match, nil
}

// GetClusterCIDR returns the known cluster CIDR blocks. kubeadm's service subnet defaults to
// 10.96.0.0/12 when unset, but it has no pod subnet default -- that is entirely CNI dependent --
// so one is only returned when the engine config sets podSubnet. Trusting nothing is safer here
// than inventing a value the firewall phase would treat as authoritative.
func (d *Upstream) GetClusterCIDR(dis distro.ZarfDistro) []string {
	nodeConfig := dis.Spec.Config.Engine.Dup()

	svc := nodeConfig.DigString(config.EngineConfig, keyServiceSubnet)
	if svc == "" {
		svc = upstreamServiceCIDR
	}

	cidrs := []string{svc}
	if pod := nodeConfig.DigString(config.EngineConfig, keyPodSubnet); pod != "" {
		cidrs = append(cidrs, pod)
	}
	return cidrs
}

// ConfigureEngine does distro specific configuration on a host. kubeadm has no equivalent of
// rke2/k3s's single config.yaml write: a controller is bootstrapped with `kubeadm init` and a
// worker joins with `kubeadm join`, neither of which is implemented yet.
func (d *Upstream) ConfigureEngine(_ context.Context, _ *cluster.ZarfHost, _ cluster.ZarfRuntimeMeta, _ distro.ZarfDistro) error {
	return fmt.Errorf("%w: kubeadm bootstrap", ErrNotImplemented)
}

// DesiredFiles returns the full set of engine config files this distro would write. Containerd's
// config.toml and crictl.yaml land here once upstream owns the container runtime configuration;
// until then there is honestly nothing to desire, so this returns an empty set rather than an
// error.
func (d *Upstream) DesiredFiles(_ *cluster.ZarfHost, _ cluster.ZarfRuntimeMeta, _ distro.ZarfDistro) (map[string]DesiredFile, error) {
	return nil, nil
}

// ManagedDirs returns the directories on a host cargoship prunes. Upstream manages none yet.
func (d *Upstream) ManagedDirs() []ManagedDir {
	return nil
}

// CleanupPaths returns the paths an uninstall removes from a host: the kubernetes config
// directory, the kubelet data directory, and the staged package directory, all of which
// upstream owns outright.
func (d *Upstream) CleanupPaths() []string {
	return removablePaths(d.DataDirPath(), d.ConfigPath(), d.PackageStagingDir())
}

// JoinTokenPathAgent returns the path of the token to join the cluster as a worker. kubeadm has
// no static agent-join-token file -- tokens are short-lived and minted on demand with
// `kubeadm token create` -- so there is nothing to point at yet.
func (d *Upstream) JoinTokenPathAgent() string {
	return ""
}

// StopControllerService stops the controller service on the host. Controller and worker are the
// same kubelet service, and unlike rke2/k3s there is no embedded containerd or killall script to
// account for.
func (d *Upstream) StopControllerService(h *cluster.ZarfHost) error {
	return h.StopService(context.Background(), d.GetControllerService())
}

// StopWorkerService stops the worker service on the host.
func (d *Upstream) StopWorkerService(h *cluster.ZarfHost) error {
	return h.StopService(context.Background(), d.GetWorkerService())
}
