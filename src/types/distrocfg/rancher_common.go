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

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/config"
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
	controllerArgs = []string{
		keyKubeAPI,
		keyKubeConMan,
		keyKubeScheduler,
		keyETCD,
	}
)

const (
	//keep-sorted start
	keyAPIVersion    = "apiVersion"
	keyAgentToken    = "agent-token-file"
	keyAudit         = "audit-policy-file"
	keyCIDRPod       = "cluster-cidr"
	keyCIDRSVC       = "service-cidr"
	keyDataDir       = "data-dir"
	keyETCD          = "etcd-arg"
	keyKind          = "kind"
	keyKubeAPI       = "kube-apiserver-arg"
	keyKubeConMan    = "kube-controller-manager-arg"
	keyKubeScheduler = "kube-scheduler-arg"
	keyMetadata      = "metadata"
	keyNodeLabel     = "node-label"
	keyNodeName      = "node-name"
	keyNodeTaint     = "node-taint"
	keyPodSec        = "pod-security-admission-config-file"
	keyServer        = "server"
	keySpec          = "spec"
	keyTLS           = "tls-san"
	keyToken         = "token-file"
	//keep-sorted end
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

// ConfigureEngine does distro specific configuration on a host
func (d *RancherCommon) ConfigureEngine(ctx context.Context, host cluster.ZarfHost, run cluster.ZarfRuntimeMeta, dis distro.ZarfDistro) error {
	nodeConfig := dis.Spec.Config.Engine.Dup()

	nodeConfig.DigMapping(config.EngineConfig)[keyNodeName] = host.Hostname
	nodeConfig.DigMapping(config.EngineConfig)[keyDataDir] = d.Data

	if len(host.NodeLabels) > 0 {
		nodeConfig.DigMapping(config.EngineConfig)[keyNodeLabel] = NodeLabelsMapToList(host.NodeLabels)
	}
	if len(host.NodeTaints) > 0 {
		nodeConfig.DigMapping(config.EngineConfig)[keyNodeTaint] = host.NodeTaints
	}

	if host.IsController() {
		nodeConfig.DigMapping(config.EngineConfig)[keyTLS] = run.ControllerTLS
		nodeConfig.DigMapping(config.EngineConfig)[keyToken] = d.JoinTokenPath()
		nodeConfig.DigMapping(config.EngineConfig)[keyAgentToken] = d.JoinTokenPathAgent()

		if !host.FileExist(d.JoinTokenPath()) {
			if err := host.WriteFile(d.JoinTokenPath(), run.ControllerToken, "0600"); err != nil {
				logger.From(ctx).Warn("failed to write file", "host", host)
				return err
			}
		} else {
			if value, err := host.ReadFile(d.JoinTokenPath()); err != nil {
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
			d.writeYAML(ctx, host, config, fmt.Sprintf("%s/server/manifests/%s-config.yaml", d.Data, k))
		}

		if nodeConfig.DigString(config.EngineConfig, "profile") != "" {
			if v, err := host.ExecOutput("getent passwd etcd"); err != nil && v == "" {
				logger.From(ctx).Info("need to create an etcd user for profile", "host", host)
				host.Execf("useradd --no-create-home --shell /sbin/nologin --system --user-group etcd", exec.Sudo(host))
			}
		}
	} else {
		nodeConfig.DigMapping(config.EngineConfig)[keyToken] = d.JoinTokenPathAgent()
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
		if value, err := host.ReadFile(d.JoinTokenPathAgent()); err != nil {
			run.AgentToken = value
		}
	}

	if len(nodeConfig.DigMapping(config.EngineAudit)) > 0 {
		nodeConfig.DigMapping(config.EngineAudit)[keyKind] = "Policy"
		nodeConfig.DigMapping(config.EngineAudit)[keyAPIVersion] = "audit.k8s.io/v1"
		audit := filepath.Join(filepath.Dir(d.Config), "audit.yaml")
		d.writeYAML(ctx, host, nodeConfig.DigMapping(config.EngineAudit), audit)
		nodeConfig.DigMapping(config.EngineConfig)[keyAudit] = audit
	}

	if len(nodeConfig.DigMapping(config.EnginePSS)) > 0 {
		nodeConfig.DigMapping(config.EnginePSS)[keyKind] = "AdmissionConfiguration"
		nodeConfig.DigMapping(config.EnginePSS)[keyAPIVersion] = "apiserver.config.k8s.io/v1"
		pss := filepath.Join(filepath.Dir(d.Config), "pss.yaml")
		d.writeYAML(ctx, host, nodeConfig.DigMapping(config.EnginePSS), pss)
		nodeConfig.DigMapping(config.EngineConfig)[keyPodSec] = pss
	}

	return d.writeYAML(ctx, host, nodeConfig.DigMapping(config.EngineConfig), d.Config)
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
	match := versionRegex.FindString(out)
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
	if h.Configurer.CommandExist(h, killall) {
		out, err := h.ExecOutput(killall, exec.Sudo(h))
		log.Warnf("%s", out)
		return err
	}
	return nil
}
