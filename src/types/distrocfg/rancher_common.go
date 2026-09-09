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
	"encoding/base64"
	"fmt"
	"path/filepath"
	"regexp"
	"time"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/pkg/engineconfig/gen"
	"github.com/k0sproject/dig"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/log"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// RancherCommon is a parent object for both RKE2 and k3s distros
type RancherCommon struct {
	Common
}

var (
	// rancherVersionRegex matches the version rke2 and k3s print, which always carries a
	// distro suffix -- v1.36.4+k3s1, v1.35.8+rke2r1. Other distros print their version
	// differently and parse it themselves.
	rancherVersionRegex = regexp.MustCompile(`v?[0-9]+\.[0-9]+\.[0-9]+\+[a-z0-9]+`)

	controllerArgs = []string{
		keyKubeAPI,
		keyKubeConMan,
		keyKubeScheduler,
		keyETCD,
	}
)

// Keys used in the files this file writes, kept in one place so a rename cannot silently
// change what an engine reads. Three files are involved: config.yaml (engine flags), the
// manifests written under the data directory, and registries.yaml (mirrors and credentials,
// documented at https://docs.rke2.io/install/private_registry). Names are sorted, so keys for
// different files sit next to each other.
const (
	// keyAPIVersion is the apiVersion of a manifest cargoship writes: a HelmChartConfig, the
	// audit policy, or the pod security admission configuration.
	keyAPIVersion = "apiVersion"
	// keyAgentToken points config.yaml at the file holding the token an agent joins with.
	keyAgentToken = "agent-token-file"
	// keyAudit points config.yaml at the audit policy file the apiserver loads.
	keyAudit = "audit-policy-file"
	// keyAuth names both the auth section of a configs entry and, inside it, the base64
	// "username:password" credential. The engines spell both the same way.
	keyAuth = "auth"
	// keyCIDRPod is the config.yaml key holding the pod network range.
	keyCIDRPod = "cluster-cidr"
	// keyCIDRSVC is the config.yaml key holding the service network range.
	keyCIDRSVC = "service-cidr"
	// keyConfigs is the registries.yaml section holding auth per mirror, keyed by the mirror's
	// host and port rather than by the registry name.
	keyConfigs = "configs"
	// keyDataDir is the config.yaml key holding the directory the engine keeps its state in.
	keyDataDir = "data-dir"
	// keyETCD passes flags through to etcd. Controllers only.
	keyETCD = "etcd-arg"
	// keyEndpoint is the mirrors key holding the addresses to pull from, in order.
	keyEndpoint = "endpoint"
	// keyIdentityToken is the auth key holding a token the registry issued, as opposed to a
	// password. The engine exchanges it with the registry for a bearer token.
	keyIdentityToken = "identity_token"
	// keyKind is the kind of a manifest cargoship writes, alongside keyAPIVersion.
	keyKind = "kind"
	// keyKubeAPI passes flags through to the apiserver. Controllers only.
	keyKubeAPI = "kube-apiserver-arg"
	// keyKubeConMan passes flags through to the controller manager. Controllers only.
	keyKubeConMan = "kube-controller-manager-arg"
	// keyKubeScheduler passes flags through to the scheduler. Controllers only.
	keyKubeScheduler = "kube-scheduler-arg"
	// keyMetadata is the metadata of a manifest cargoship writes: its name and namespace.
	keyMetadata = "metadata"
	// keyMirrors is the registries.yaml section mapping a registry name to where pulls for it
	// go instead.
	keyMirrors = "mirrors"
	// keyNodeLabel is the config.yaml key holding the labels the node registers with.
	keyNodeLabel = "node-label"
	// keyNodeName is the config.yaml key holding the name the node registers under.
	keyNodeName = "node-name"
	// keyNodeTaint is the config.yaml key holding the taints the node registers with.
	keyNodeTaint = "node-taint"
	// keyPassword is the auth key holding a registry password. Written only when there is no
	// username to pair it with, since a pair is encoded under keyAuth instead.
	keyPassword = "password"
	// keyPodSec points config.yaml at the pod security admission configuration file.
	keyPodSec = "pod-security-admission-config-file"
	// keyRewrite is the mirrors key mapping a regex to a replacement, applied to the image name
	// before it is pulled from that mirror.
	keyRewrite = "rewrite"
	// keyServer is the config.yaml key holding the URL of the controller a joining node
	// registers with.
	keyServer = "server"
	// keySpec is the spec of a manifest cargoship writes, holding the Helm values.
	keySpec = "spec"
	// keyTLS is the config.yaml key listing the extra names and addresses that go on the
	// controller's TLS certificate.
	keyTLS = "tls-san"
	// keyTokenFile points config.yaml at the file holding the token a controller joins with.
	keyTokenFile = "token-file"
	// keyUsername is the auth key holding a registry username. Written only when there is no
	// password to pair it with, since a pair is encoded under keyAuth instead.
	keyUsername = "username"
)

// Both RKE2 and k3s share similar logic on how to configure the Kubernetes Engine.
// config.yaml:
// we look at `.spec.config.engine.config`, to determine the the globally shared config across all nodes
// audit-config.yaml:
// we look at `.spec.config.engine.audit`, to determine the kubelet audit settings. There is no validation done at this time, please reference: https://kubernetes.io/docs/reference/config-api/apiserver-audit.v1/#audit-k8s-io-v1-Policy
// if `.spec.config.engine.audit` is present we will add/overwrite "audit-policy-file" with the value of "/etc/rancher/(rke2|k3s)/audit-config.yaml"
// pss.yaml:
// we look at `.spec.config.engine.podSecurity`, to determine the "pod security admission" that will be enforced by the kubelet. There is no validation done at this time, please reference: https://kubernetes.io/docs/concepts/security/pod-security-admission/
// if `.spec.config.engine.podSecurity` is present we will add/overwrite "pod-security-admission-config-file" with the value of "/etc/rancher/(rke2|k3s)/pss.yaml"
// if `.spec.config.engine.manifest` is present we will create files under
// registries.yaml:
// we look at the cluster document's `.spec.config.registries`, to determine registry mirrors and their credentials.
// if any are present we write "/etc/rancher/(rke2|k3s)/registries.yaml" with a "mirrors" entry per registry, a
// "rewrite" entry per mirror when `.proxy.rewrite` is set, and a "configs" entry with auth when credentials are set.

// ConfigureEngine does distro specific configuration on a host
func (d *RancherCommon) ConfigureEngine(ctx context.Context, host cluster.ZarfHost, run cluster.ZarfRuntimeMeta, dis distro.ZarfDistro) error {
	nodeConfig := dis.Spec.Config.Engine.Dup()

	nodeConfig.DigMapping(config.EngineConfig)[keyNodeName] = host.Hostname
	nodeConfig.DigMapping(config.EngineConfig)[keyDataDir] = d.Data

	if len(host.Engine.NodeLabels) > 0 {
		nodeConfig.DigMapping(config.EngineConfig)[keyNodeLabel] = NodeLabelsMapToList(host.Engine.NodeLabels)
	}
	if len(host.Engine.NodeTaints) > 0 {
		nodeConfig.DigMapping(config.EngineConfig)[keyNodeTaint] = host.Engine.NodeTaints
	}

	if host.IsController() {
		nodeConfig.DigMapping(config.EngineConfig)[keyTLS] = run.ControllerTLS
		nodeConfig.DigMapping(config.EngineConfig)[keyTokenFile] = d.JoinTokenPath()
		nodeConfig.DigMapping(config.EngineConfig)[keyAgentToken] = d.JoinTokenPathAgent()

		if !host.FileExist(d.JoinTokenPath()) {
			if err := host.WriteFile(d.JoinTokenPath(), run.ControllerToken, "0600"); err != nil {
				logger.From(ctx).Warn("failed to write file", "host", host)
				return err
			}
		} else {
			if value, err := host.ReadFile(d.JoinTokenPath()); err == nil {
				run.ControllerToken = value
			}
		}

		if !host.Metadata.IsLeader {
			nodeConfig.DigMapping(config.EngineConfig)[keyServer] = fmt.Sprintf("https://%s:9345", run.Leader.Configurer.LongHostname(run.Leader))
		}

		for k, v := range nodeConfig.DigMapping(config.EngineManifest) {
			config := dig.Mapping{}
			config[keyAPIVersion] = "helm.cattle.io/v1"
			config[keyKind] = "HelmChartConfig"
			config[keyMetadata] = map[string]string{
				"name":      k,
				"namespace": "kube-system",
			}
			config[keySpec] = map[string]string{
				"valuesContent": fmt.Sprint(v),
			}
			err := d.writeYAML(ctx, host, config, fmt.Sprintf("%s/server/manifests/%s-config.yaml", d.Data, k))
			if err != nil {
				logger.From(ctx).Warn("failed to write", "file", fmt.Sprintf("%s-config.yaml", k))
			}
		}

		if nodeConfig.DigString(config.EngineConfig, "profile") != "" {
			if v, err := host.ExecOutput("getent passwd etcd"); err != nil && v == "" {
				logger.From(ctx).Info("need to create an etcd user for profile", "host", host.Connection.String())
				// need to relook into how to structure the `sudo` section
				err := host.Execf("sudo useradd --no-create-home --shell /sbin/nologin --system --user-group etcd")
				if err != nil {
					logger.From(ctx).Warn("failed to create", "user", "etcd")
				}
			}
		}
	} else {
		nodeConfig.DigMapping(config.EngineConfig)[keyTokenFile] = d.JoinTokenPathAgent()
		for _, v := range controllerArgs {
			delete(nodeConfig.DigMapping(config.EngineConfig), v)
		}
		nodeConfig.DigMapping(config.EngineConfig)[keyServer] = fmt.Sprintf("https://%s:9345", run.LoadBalancer)
	}
	if !host.FileExist(d.JoinTokenPathAgent()) {
		if err := host.WriteFile(d.JoinTokenPathAgent(), run.AgentToken, "0600"); err != nil {
			logger.From(ctx).Warn("failed to write file", "host", host)
			return err
		}
	} else {
		if value, err := host.ReadFile(d.JoinTokenPathAgent()); err == nil {
			run.AgentToken = value
		}
	}

	if len(nodeConfig.DigMapping(config.EngineAudit)) > 0 {
		nodeConfig.DigMapping(config.EngineConfig)[keyAudit] = filepath.Join(filepath.Dir(d.Config), "audit.yaml")
	}
	if len(nodeConfig.DigMapping(config.EnginePSS)) > 0 {
		nodeConfig.DigMapping(config.EngineConfig)[keyPodSec] = filepath.Join(filepath.Dir(d.Config), "pss.yaml")
	}

	// Nodes that already have the engine running are left alone here; config drift
	// (registries/audit/pss) on those is rolled out via the engine-config-sync phases,
	// which pair the write with a drain/restart/uncordon of the node.
	if !d.engineServiceRunning(&host) {
		desired, err := d.DesiredFiles(host, run, dis)
		if err != nil {
			logger.From(ctx).Warn("failed to render desired files", "host", host)
		}
		for path, content := range desired {
			if err := host.WriteFile(path, string(content), "0600"); err != nil {
				logger.From(ctx).Warn("failed to write", "file", path)
			}
		}
	}

	d.validateEngineConfig(ctx, dis.Spec.Version, host.IsController(), nodeConfig.DigMapping(config.EngineConfig))

	return d.writeYAML(ctx, host, nodeConfig.DigMapping(config.EngineConfig), d.Config)
}

// validateEngineConfig drops any config.yaml keys that aren't part of the flag set
// mage generate:engineConfig extracted for this distro at the given engine version's minor
// release (see src/pkg/engineconfig/gen), logging each one removed at debug. If no generated
// config exists for this distro/version -- e.g. a version nobody has pulled/generated yet --
// there's nothing to check against, so it warns once and leaves cfg untouched, falling back to
// writing it exactly as before this check existed.
func (d *RancherCommon) validateEngineConfig(ctx context.Context, version string, isController bool, cfg dig.Mapping) {
	entry, ok := gen.Lookup(d.ID, version)
	if !ok {
		logger.From(ctx).Warn("no generated engine config schema for this distro/version, skipping config key validation", "distro", d.ID, "version", version)
		return
	}

	target := entry.Agent
	if isController {
		target = entry.Server
	}

	valid := gen.Keys(target)
	for k := range cfg {
		if _, ok := valid[k]; !ok {
			logger.From(ctx).Debug("engine config key not recognized for this distro/version, dropping it from config.yaml", "distro", d.ID, "version", version, "key", k)
			delete(cfg, k)
		}
	}
}

func (d *RancherCommon) engineServiceRunning(h *cluster.ZarfHost) bool {
	service := d.GetWorkerService()
	if h.IsController() {
		service = d.GetControllerService()
	}
	return h.Configurer.ServiceIsRunning(h, service)
}

// DesiredFiles returns the desired content of registries.yaml, audit.yaml, and pss.yaml for
// the given host/run/dis, keyed by their full destination path. Content is identical across
// hosts of the same run (no host-varying fields are involved), unlike config.yaml.
func (d *RancherCommon) DesiredFiles(_ cluster.ZarfHost, run cluster.ZarfRuntimeMeta, dis distro.ZarfDistro) (map[string][]byte, error) {
	files := map[string][]byte{}

	if len(run.Registries) > 0 {
		b, err := marshalRegistriesYAML(buildRegistriesConfig(run.Registries))
		if err != nil {
			return nil, err
		}
		files[filepath.Join(filepath.Dir(d.Config), "registries.yaml")] = b
	}

	nodeConfig := dis.Spec.Config.Engine.Dup()

	if audit := nodeConfig.DigMapping(config.EngineAudit); len(audit) > 0 {
		audit[keyKind] = "Policy"
		audit[keyAPIVersion] = "audit.k8s.io/v1"
		b, err := marshalYAML(audit)
		if err != nil {
			return nil, err
		}
		files[filepath.Join(filepath.Dir(d.Config), "audit.yaml")] = b
	}

	if pss := nodeConfig.DigMapping(config.EnginePSS); len(pss) > 0 {
		pss[keyKind] = "AdmissionConfiguration"
		pss[keyAPIVersion] = "apiserver.config.k8s.io/v1"
		b, err := marshalYAML(pss)
		if err != nil {
			return nil, err
		}
		files[filepath.Join(filepath.Dir(d.Config), "pss.yaml")] = b
	}

	return files, nil
}

// buildRegistriesConfig builds the registry mapping (mirrors/configs) rke2 and k3s read from
// registries.yaml, based on the registry mirrors configured in `.spec.config.registries`.
//
// The key names are the ones wharfie -- the library both engines parse this file with --
// unmarshals: auth, username, password, and identity_token under auth, as documented at
// https://docs.rke2.io/install/private_registry. Unmarshalling is not strict, so a key spelled
// any other way is dropped silently and the engine goes on to pull anonymously, which surfaces
// far away from here as a 401.
func buildRegistriesConfig(registries []cluster.ZarfClusterRegistries) dig.Mapping {
	mirrors := dig.Mapping{}
	configs := dig.Mapping{}

	for _, reg := range registries {
		// Credentials belong to whichever host the pull actually goes to: the mirror when there
		// is one, and the registry itself when there is not. A registry with credentials but no
		// mirror is still worth writing -- it authenticates a direct pull -- so it gets a configs
		// entry without a mirrors entry rather than one keyed by an empty host.
		host := reg.ConfigHost()
		if endpoint := reg.MirrorEndpoint(); endpoint != "" {
			mirror := dig.Mapping{
				keyEndpoint: []string{endpoint},
			}
			if len(reg.Proxy.Rewrite) > 0 {
				mirror[keyRewrite] = reg.Proxy.Rewrite
			}
			mirrors[string(reg.Name)] = mirror
		}

		if auth := registryAuth(reg.Authentication); len(auth) > 0 {
			configs[host] = dig.Mapping{keyAuth: auth}
		}
	}

	result := dig.Mapping{}
	if len(mirrors) > 0 {
		result[keyMirrors] = mirrors
	}
	if len(configs) > 0 {
		result[keyConfigs] = configs
	}
	return result
}

// registryAuth renders one registry's credentials for a configs entry.
//
// A username and password are encoded into a single credential -- base64 of "username:password"
// -- and written under the "auth" key, which is what both engines document as "authentication
// token of the private registry basic auth". Encoding here keeps one spelling of a credential on
// the node rather than two, and keeps a password from sitting in registries.yaml as plain text.
// Base64 is an encoding rather than encryption, so the file is still a secret either way.
//
// A token given directly is a different credential -- the registry issues it, and it is not a
// base64 pair -- so it is written under identity_token, the key the engine exchanges for a
// bearer token, instead of being passed off as basic auth.
//
// A username without a password (or the reverse) cannot be encoded into a pair, so it is written
// under its own key and left for the engine to complete or reject.
func registryAuth(a cluster.ZarfClusterRegistryAuth) dig.Mapping {
	auth := dig.Mapping{}
	switch {
	case a.Token != "":
		auth[keyIdentityToken] = a.Token
	case a.Username != "" && a.Password != "":
		auth[keyAuth] = base64.StdEncoding.EncodeToString([]byte(a.Username + ":" + a.Password))
	default:
		if a.Username != "" {
			auth[keyUsername] = a.Username
		}
		if a.Password != "" {
			auth[keyPassword] = a.Password
		}
	}
	return auth
}

// GetClusterCIDR returns a string array with the all the known cluster cidr blocks
func (d *RancherCommon) GetClusterCIDR(dis distro.ZarfDistro) []string {
	nodeConfig := dis.Spec.Config.Engine.Dup()
	pod := nodeConfig.DigString(config.EngineConfig, keyCIDRPod)
	if pod == "" {
		pod = "10.42.0.0/16"
	}
	svc := nodeConfig.DigString(config.EngineConfig, keyCIDRSVC)
	if svc == "" {
		svc = "10.43.0.0/16"
	}

	return []string{
		pod,
		svc,
	}
}

// CleanupPaths returns the paths an uninstall removes from a host: the engine data
// directory and the config directory, both of which rke2 and k3s own outright.
func (d *RancherCommon) CleanupPaths() []string {
	return removablePaths(d.DataDirPath(), filepath.Dir(d.Config))
}

// JoinTokenPathAgent returns the path of the token to join the cluster.
// Distro's like RKE2 and K3S allow for agent tokens, so this allows for some level of access control if a node is allowed to be a controller or an agent.
func (d *RancherCommon) JoinTokenPathAgent() string {
	return filepath.Join(filepath.Dir(d.Token), "agent-token")
}

// DistroCmdf returns a string that can be used to execute commands on the core engine binary
func (d *RancherCommon) DistroCmdf(template string, args ...any) string {
	return fmt.Sprintf("%s %s", d.BinaryPath(), fmt.Sprintf(template, args...))
}

// RunningVersion returns the version of the distro being ran, if the engine is not running it throws an "ErrVersionNotDetected" error
func (d *RancherCommon) RunningVersion(host cluster.ZarfHost) (string, error) {
	bin, err := host.Configurer.LookPath(&host, d.Binary)
	if err != nil {
		return "", ErrVersionNotDetected
	}
	out, err := host.ExecOutputf(`%s --version`, bin)
	if err != nil {
		return "", ErrVersionNotDetected
	}
	match := rancherVersionRegex.FindString(out)
	if match == "" {
		return "", ErrVersionNotDetected
	}
	return match, nil
}

func (d *RancherCommon) stopService(h *cluster.ZarfHost, ser string, killall string) error {
	log.Debugf("trying to stop %s", ser)
	if h.Configurer.ServiceIsRunning(h, ser) {
		if err := h.Configurer.StopService(h, ser); err != nil {
			return err
		}
	}
	cache := false
	cacheFile := fmt.Sprintf("%s/agent/images/.cache.json", d.Data)
	if h.Configurer.FileExist(h, cacheFile) {
		cache = true
		if err := h.Configurer.DeleteFile(h, cacheFile); err != nil {
			return err
		}
	}
	if h.Configurer.CommandExist(h, killall) {
		return h.Exec(killall, exec.Sudo(h))
	}
	if cache {
		h.Configurer.Touch(h, cacheFile, time.Unix(0, 0)) //nolint:errcheck
	}
	return nil
}
