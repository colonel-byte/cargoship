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

type RancherCommon struct {
	Common
}

var (
	controllerArgs = []string{
		key_kube_api,
		key_kube_cont_manager,
		key_kube_scheduler,
		key_etcd,
	}
)

const (
	//keep-sorted start
	key_agent_token       = "agent-token-file"
	key_api_version       = "apiVersion"
	key_audit             = "audit-policy-file"
	key_cidr_pod          = "cluster-cidr"
	key_cidr_svc          = "service-cidr"
	key_data_dir          = "data-dir"
	key_etcd              = "etcd-arg"
	key_kind              = "kind"
	key_kube_api          = "kube-apiserver-arg"
	key_kube_cont_manager = "kube-controller-manager-arg"
	key_kube_scheduler    = "kube-scheduler-arg"
	key_metadata          = "metadata"
	key_node_label        = "node-label"
	key_node_name         = "node-name"
	key_node_taint        = "node-taint"
	key_pod_sec           = "pod-security-admission-config-file"
	key_server            = "server"
	key_spec              = "spec"
	key_tls               = "tls-san"
	key_token             = "token-file"
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

// ConfigureEngine implements Distro.
func (d *RancherCommon) ConfigureEngine(ctx context.Context, host cluster.ZarfHost, run cluster.ZarfRuntimeMeta, dis distro.ZarfDistro) error {
	nodeConfig := dis.Spec.Config.Engine.Dup()

	nodeConfig.DigMapping(config.EngineConfig)[key_node_name] = host.Hostname
	nodeConfig.DigMapping(config.EngineConfig)[key_data_dir] = d.Data

	if len(host.NodeLabels) > 0 {
		nodeConfig.DigMapping(config.EngineConfig)[key_node_label] = NodeLabelsMapToList(host.NodeLabels)
	}
	if len(host.NodeTaints) > 0 {
		nodeConfig.DigMapping(config.EngineConfig)[key_node_taint] = host.NodeTaints
	}

	if host.IsController() {
		nodeConfig.DigMapping(config.EngineConfig)[key_tls] = run.ControllerTLS
		nodeConfig.DigMapping(config.EngineConfig)[key_token] = d.JoinTokenPath()
		nodeConfig.DigMapping(config.EngineConfig)[key_agent_token] = d.JoinTokenPathAgent()

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
			nodeConfig.DigMapping(config.EngineConfig)[key_server] = fmt.Sprintf("https://%s:9345", run.Leader.Configurer.LongHostname(run.Leader))
		}

		for k, v := range nodeConfig.DigMapping(config.EngineManifest) {
			config := dig.Mapping{}
			config[key_api_version] = "helm.cattle.io/v1"
			config[key_kind] = "HelmChartConfig"
			config[key_metadata] = map[string]string{
				"name":      k,
				"namespace": "kube-system",
			}
			config[key_spec] = map[string]string{
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
		nodeConfig.DigMapping(config.EngineConfig)[key_token] = d.JoinTokenPathAgent()
		for _, v := range controllerArgs {
			delete(nodeConfig.DigMapping(config.EngineConfig), v)
		}
		nodeConfig.DigMapping(config.EngineConfig)[key_server] = fmt.Sprintf("https://%s:9345", run.LoadBalancer)
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
		nodeConfig.DigMapping(config.EngineAudit)[key_kind] = "Policy"
		nodeConfig.DigMapping(config.EngineAudit)[key_api_version] = "audit.k8s.io/v1"
		audit := filepath.Join(filepath.Dir(d.Config), "audit.yaml")
		d.writeYAML(ctx, host, nodeConfig.DigMapping(config.EngineAudit), audit)
		nodeConfig.DigMapping(config.EngineConfig)[key_audit] = audit
	}

	if len(nodeConfig.DigMapping(config.EnginePSS)) > 0 {
		nodeConfig.DigMapping(config.EnginePSS)[key_kind] = "AdmissionConfiguration"
		nodeConfig.DigMapping(config.EnginePSS)[key_api_version] = "apiserver.config.k8s.io/v1"
		pss := filepath.Join(filepath.Dir(d.Config), "pss.yaml")
		d.writeYAML(ctx, host, nodeConfig.DigMapping(config.EnginePSS), pss)
		nodeConfig.DigMapping(config.EngineConfig)[key_pod_sec] = pss
	}

	return d.writeYAML(ctx, host, nodeConfig.DigMapping(config.EngineConfig), d.Config)
}

func (d *RancherCommon) GetClusterCIDR(dis distro.ZarfDistro) []string {
	nodeConfig := dis.Spec.Config.Engine.Dup()
	pod := nodeConfig.DigString(config.EngineConfig, key_cidr_pod)
	if pod == "" {
		pod = "10.42.0.0/16"
	}
	svc := nodeConfig.DigString(config.EngineConfig, key_cidr_svc)
	if svc == "" {
		svc = "10.43.0.0/16"
	}

	return []string{
		pod,
		svc,
	}
}

func (d *RancherCommon) DistroCmdf(template string, args ...any) string {
	return fmt.Sprintf("%s %s", d.BinaryPath(), fmt.Sprintf(template, args...))
}

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

func (d *RancherCommon) StopService(h *cluster.ZarfHost, ser string, killall string) error {
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
