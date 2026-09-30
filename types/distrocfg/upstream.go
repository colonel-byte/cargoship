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
	"io/fs"
	"path/filepath"
	"regexp"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/types/distrocfg/registry"
	"github.com/k0sproject/dig"
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

var (
	_ Distro               = (*Upstream)(nil)
	_ ImageImporter        = (*Upstream)(nil)
	_ Bootstrapper         = (*Upstream)(nil)
	_ ManifestApplier      = (*Upstream)(nil)
	_ PreStartUpgrader     = (*Upstream)(nil)
	_ PreUninstallResetter = (*Upstream)(nil)
)

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

	svc := nodeConfig.DigString(config.EngineConfig, keyNetworking, keyServiceSubnet)
	if svc == "" {
		svc = upstreamServiceCIDR
	}

	cidrs := []string{svc}
	if pod := nodeConfig.DigString(config.EngineConfig, keyNetworking, keyPodSubnet); pod != "" {
		cidrs = append(cidrs, pod)
	}
	return cidrs
}

// ConfigureEngine writes DesiredFiles' output to host: containerd's config, crictl.yaml,
// registry hosts.toml, and the kubeadm-config.yaml this host's role needs. Unlike
// RancherCommon.ConfigureEngine there is no already-running guard -- kubeadm-config.yaml is meant
// to be rewritten every run (Bootstrap rewrites it again with the real join token right before
// `kubeadm join`), so an unconditional overwrite is correct here, not a gap.
func (d *Upstream) ConfigureEngine(ctx context.Context, host *cluster.ZarfHost, run cluster.ZarfRuntimeMeta, dis distro.ZarfDistro) error {
	desired, err := d.DesiredFiles(ctx, host, run, dis)
	if err != nil {
		return err
	}
	for path, file := range desired {
		if err := host.WriteFile(path, string(file.Content), file.Mode); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}
	return nil
}

// DesiredFiles returns the full set of engine config files this distro would write: containerd's
// config.toml, crictl.yaml, and a hosts.toml plus any CA certificate per registry cargoship
// configures a mirror, credential, or TLS setting for.
func (d *Upstream) DesiredFiles(ctx context.Context, host *cluster.ZarfHost, run cluster.ZarfRuntimeMeta, dis distro.ZarfDistro) (map[string]DesiredFile, error) {
	files := map[string]DesiredFile{}

	configTOML, err := marshalTOML(buildContainerdConfig(dis))
	if err != nil {
		return nil, err
	}
	files[containerdConfigPath] = DesiredFile{Content: configTOML, Mode: modeConfigFile}

	crictlYAML, err := marshalYAML(buildCrictlConfig())
	if err != nil {
		return nil, err
	}
	files[crictlConfigPath] = DesiredFile{Content: crictlYAML, Mode: modeConfigFile}

	for name, hostsConfig := range buildContainerdHostsConfigs(run.Registries) {
		hostsTOML, err := marshalTOML(hostsConfig)
		if err != nil {
			return nil, err
		}
		files[containerdHostsPath(name)] = DesiredFile{Content: hostsTOML, Mode: modeRegistries}
	}

	for path, ca := range registryCAFiles(run.Registries) {
		files[path] = DesiredFile{Content: ca, Mode: modeConfigFile}
	}

	nodeConfig := dis.Spec.Config.Engine.Dup()

	if audit := nodeConfig.DigMapping(config.EngineAudit); len(audit) > 0 {
		audit[keyKind] = "Policy"
		audit[keyAPIVersion] = "audit.k8s.io/v1"
		b, err := marshalYAML(audit)
		if err != nil {
			return nil, err
		}
		files[upstreamAuditFilePath] = DesiredFile{Content: b, Mode: modeConfigFile}
	}

	if pss := nodeConfig.DigMapping(config.EnginePSS); len(pss) > 0 {
		pss[keyKind] = "AdmissionConfiguration"
		pss[keyAPIVersion] = "apiserver.config.k8s.io/v1"
		b, err := marshalYAML(pss)
		if err != nil {
			return nil, err
		}
		files[upstreamPSSFilePath] = DesiredFile{Content: b, Mode: modeConfigFile}
	}

	var kubeadmDocs []dig.Mapping
	if host.Metadata.IsLeader {
		kubeadmDocs = []dig.Mapping{buildClusterConfiguration(ctx, dis, run), buildInitConfiguration(host), buildKubeletConfiguration()}
	} else {
		kubeadmDocs = []dig.Mapping{buildJoinConfiguration(host, run), buildKubeletConfiguration()}
	}
	kubeadmYAML, err := marshalYAMLDocs(kubeadmDocs...)
	if err != nil {
		return nil, err
	}
	files[kubeadmConfigPath] = DesiredFile{Content: kubeadmYAML, Mode: modeConfigFile}

	if path, df, ok, err := DistroReleaseDesiredFile(dis, files); err != nil {
		return nil, err
	} else if ok {
		files[path] = df
	}

	return files, nil
}

// ManagedDirs returns the directories on a host cargoship prunes: the shared CA directory
// registryCAFiles writes to. The per-registry hosts.toml directories under containerdCertsDir are
// not included -- ManagedDir only prunes files directly inside a directory, not ones nested a
// level down in a registry's own subdirectory, so a removed registry's hosts.toml is left behind
// rather than risk pruning the wrong thing.
func (d *Upstream) ManagedDirs() []ManagedDir {
	return []ManagedDir{
		{Path: registryTLSDir},
	}
}

// ImportImages imports every image tarball staged under path into containerd's k8s.io image
// store. Unlike rke2/k3s, upstream's containerd has no agent watching that directory on its own,
// so cargoship has to trigger the import itself.
func (d *Upstream) ImportImages(host *cluster.ZarfHost, path string) error {
	if !host.FileExist(path) {
		return nil
	}

	return fs.WalkDir(host.Sudo().FS(), path, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		cmd := fmt.Sprintf("ctr -n k8s.io images import %s", filepath.Join(path, entry.Name()))
		if _, err := host.Sudo().ExecOutput(cmd); err != nil {
			return fmt.Errorf("importing image tarball %s: %w", p, err)
		}
		return nil
	})
}

// ManifestPaths returns the on-host manifest paths the package declares for cargoship to kubectl
// apply, e.g. a CNI. Delivery of those files is Config.Files' job; this is only the list of what
// to apply.
func (d *Upstream) ManifestPaths(dis distro.ZarfDistro) []string {
	return dis.Spec.Config.Manifests
}

// CleanupPaths returns the paths an uninstall removes from a host: the kubernetes config
// directory, the kubelet data directory, and the staged package directory, all of which
// upstream owns outright.
func (d *Upstream) CleanupPaths() []string {
	return removablePaths(d.DataDirPath(), d.ConfigPath(), d.PackageStagingDir(), "/etc/cni/net.d")
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
