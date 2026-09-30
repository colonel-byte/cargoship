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
	"strings"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/engineconfig/extract"
	"github.com/colonel-byte/cargoship/pkg/engineconfig/gen"
	"github.com/k0sproject/dig"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// For Rancher, `.spec.config.engine.config` is a flat bag of CLI flags (`cluster-cidr`,
// `kube-apiserver-arg`, ...). Upstream has no single engine binary those flags belong to --
// kubeadm, kubelet, and the static-pod control plane each read their own document -- so
// `.spec.config.engine.config` is instead shaped like kubeadm's own ClusterConfiguration body:
// `networking.podSubnet`/`networking.serviceSubnet` rather than `cluster-cidr`/`service-cidr`,
// `apiServer.extraArgs`/`controllerManager.extraArgs`/`scheduler.extraArgs` (each a list of
// `{name, value}` pairs -- v1beta4's shape, not a map) rather than `kube-apiserver-arg`. The same
// cluster document key means a different thing per distro type; this is deliberate, not
// accidental drift, and callers reading `.spec.config.engine.config` must check `.spec.type`
// before assuming either shape.

const (
	// kubeadmConfigPath is where cargoship renders the kubeadm config documents. Nothing reads
	// this path automatically -- it is passed to `kubeadm init --config`/`kubeadm join --config`
	// explicitly, which is a distro-specific bootstrap step, not DesiredFiles' concern.
	//
	// Rewriting this file after init/join does not, by itself, change anything: kubeadm only
	// reads ClusterConfiguration/InitConfiguration/JoinConfiguration at that one-shot bootstrap
	// moment, so a drifted ClusterConfiguration/apiServer/etc. key needs `kubeadm upgrade apply
	// --config` (or hand-editing the control plane's static pod manifests) to take effect --
	// engine-config-sync's rewrite-and-restart-the-service cycle does not do that for kubeadm.
	// The KubeletConfiguration document in this same file is the exception: the kubelet re-reads
	// its own config on restart, so a kubelet key does get picked up by that cycle.
	kubeadmConfigPath = "/etc/kubernetes/kubeadm-config.yaml"
	// kubeadmAPIVersion is the kubeadm config API version cargoship renders.
	kubeadmAPIVersion = "kubeadm.k8s.io/v1beta4"
	// kubeletConfigAPIVersion is the kubelet component config API version cargoship renders.
	kubeletConfigAPIVersion = "kubelet.config.k8s.io/v1beta1"

	// keyNetworking is the ClusterConfiguration section holding the cluster's pod/service CIDRs.
	keyNetworking = "networking"

	// upstreamAuditFilePath and upstreamPSSFilePath are where DesiredFiles writes the audit
	// policy and pod security admission config, when the engine config sets one. They live
	// under Upstream's own config directory rather than derived from a Common field, since
	// auditPSSVolumes has no distro instance to read one off of.
	upstreamAuditFilePath = "/etc/kubernetes/audit.yaml"
	upstreamPSSFilePath   = "/etc/kubernetes/pss.yaml"
)

// extraArg renders one kubeadm v1beta4 extraArgs entry. v1beta4 changed this from a v1beta3
// string map to a list of {name, value} pairs, to allow a flag to be repeated -- passing a map
// here would silently produce a config kubeadm rejects.
func extraArg(name, value string) dig.Mapping {
	return dig.Mapping{"name": name, "value": value}
}

// buildClusterConfiguration renders kubeadm's ClusterConfiguration: the cluster-wide document a
// leader's `kubeadm init` reads. Every other host only ever sees a JoinConfiguration -- this
// object is deliberately never rendered for them.
func buildClusterConfiguration(ctx context.Context, dis distro.ZarfDistro, run cluster.ZarfRuntimeMeta) dig.Mapping {
	nodeConfig := dis.Spec.Config.Engine.Dup()
	validateNestedEngineConfig(ctx, dis.Spec.Version, nodeConfig.DigMapping(config.EngineConfig))

	svc := nodeConfig.DigString(config.EngineConfig, keyNetworking, keyServiceSubnet)
	if svc == "" {
		svc = upstreamServiceCIDR
	}
	networking := dig.Mapping{keyServiceSubnet: svc}
	if pod := nodeConfig.DigString(config.EngineConfig, keyNetworking, keyPodSubnet); pod != "" {
		networking[keyPodSubnet] = pod
	}

	cc := dig.Mapping{
		"apiVersion":           kubeadmAPIVersion,
		"kind":                 "ClusterConfiguration",
		"kubernetesVersion":    dis.Spec.Version,
		"controlPlaneEndpoint": run.LoadBalancer,
		keyNetworking:          networking,
	}

	if repo := imageRepository(nodeConfig, dis.Spec.Config.ImagesConfig.Images); repo != "" {
		cc["imageRepository"] = repo
	}

	apiServer := dig.Mapping{}
	if len(run.ControllerTLS) > 0 {
		apiServer["certSANs"] = run.ControllerTLS
	}
	apiServerArgs := extraArgsFrom(nodeConfig, "apiServer")
	if vols := auditPSSVolumes(nodeConfig, &apiServerArgs); len(vols) > 0 {
		apiServer["extraVolumes"] = vols
	}
	if len(apiServerArgs) > 0 {
		apiServer["extraArgs"] = apiServerArgs
	}
	if len(apiServer) > 0 {
		cc["apiServer"] = apiServer
	}

	if args := extraArgsFrom(nodeConfig, "controllerManager"); len(args) > 0 {
		cc["controllerManager"] = dig.Mapping{"extraArgs": args}
	}
	if args := extraArgsFrom(nodeConfig, "scheduler"); len(args) > 0 {
		cc["scheduler"] = dig.Mapping{"extraArgs": args}
	}

	return cc
}

// extraArgsFrom reads a component's extraArgs list out of the engine config, which is already
// shaped as v1beta4 expects -- a list of {name, value} mappings -- and passed through untouched.
func extraArgsFrom(nodeConfig dig.Mapping, component string) []any {
	switch v := nodeConfig.Dig(config.EngineConfig, component, "extraArgs").(type) {
	case []any:
		return v
	default:
		return nil
	}
}

// auditPSSVolumes returns the apiServer.extraVolumes hostPath mounts the audit and pod security
// admission files need, and appends the matching --audit-policy-file/--admission-control-config-file
// flag to apiServerArgs. kubeadm runs the apiserver as a static pod: a --audit-policy-file flag
// pointing at a host path does nothing unless that path is also mounted in, unlike Rancher's
// engines which run the apiserver as a plain host process with the whole filesystem visible.
func auditPSSVolumes(nodeConfig dig.Mapping, apiServerArgs *[]any) []dig.Mapping {
	var vols []dig.Mapping

	if len(nodeConfig.DigMapping(config.EngineAudit)) > 0 {
		vols = append(vols, hostPathVolume("audit-policy", upstreamAuditFilePath))
		*apiServerArgs = append(*apiServerArgs,
			extraArg("audit-policy-file", upstreamAuditFilePath),
			extraArg("audit-log-path", "-"),
		)
	}
	if len(nodeConfig.DigMapping(config.EnginePSS)) > 0 {
		vols = append(vols, hostPathVolume("pod-security", upstreamPSSFilePath))
		*apiServerArgs = append(*apiServerArgs, extraArg("admission-control-config-file", upstreamPSSFilePath))
	}

	return vols
}

// hostPathVolume renders one apiServer.extraVolumes entry mounting a host file, read only, into
// the static apiserver pod at the same path it lives at on the host.
func hostPathVolume(name, path string) dig.Mapping {
	return dig.Mapping{
		"name":      name,
		"hostPath":  path,
		"mountPath": path,
		"readOnly":  true,
		"pathType":  "File",
	}
}

// imageRepository returns the registry prefix kubeadm should pull its own control-plane images
// from. An explicit `.spec.config.engine.config.imageRepository` always wins; otherwise it is
// derived from the packaged images' own registry, the same way sandboxImage finds the packaged
// pause image, rather than falling back to kubeadm's registry.k8s.io default, which may not be
// what was actually bundled.
func imageRepository(nodeConfig dig.Mapping, images []string) string {
	if repo := nodeConfig.DigString(config.EngineConfig, "imageRepository"); repo != "" {
		return repo
	}
	for _, ref := range images {
		if idx := strings.LastIndex(ref, "/"); idx != -1 {
			return ref[:idx]
		}
	}
	return ""
}

// buildInitConfiguration renders kubeadm's InitConfiguration for the cluster leader. No
// bootstrapTokens entry is set -- kubeadm mints its own default token when init is given none,
// and deciding whether to pin one instead is a bootstrap-time concern, not a rendering one.
func buildInitConfiguration(host *cluster.ZarfHost) dig.Mapping {
	return dig.Mapping{
		"apiVersion":       kubeadmAPIVersion,
		"kind":             "InitConfiguration",
		"nodeRegistration": nodeRegistration(host),
	}
}

// buildJoinConfiguration renders kubeadm's JoinConfiguration for every host that is not the
// cluster leader. discovery.bootstrapToken.caCertHashes is left empty: generating and threading
// a real hash through ZarfRuntimeMeta is the bootstrap phase's job, not DesiredFiles' -- this
// only has to give that later step a key to fill in, not invent the value itself.
func buildJoinConfiguration(host *cluster.ZarfHost, run cluster.ZarfRuntimeMeta) dig.Mapping {
	token := run.AgentToken
	if host.IsController() {
		token = run.ControllerToken
	}

	jc := dig.Mapping{
		"apiVersion": kubeadmAPIVersion,
		"kind":       "JoinConfiguration",
		"discovery": dig.Mapping{
			"bootstrapToken": dig.Mapping{
				"token":             token,
				"apiServerEndpoint": run.LoadBalancer,
			},
		},
		"nodeRegistration": nodeRegistration(host),
	}

	if host.IsController() {
		jc["controlPlane"] = dig.Mapping{
			"localAPIEndpoint": dig.Mapping{},
		}
	}

	return jc
}

// nodeRegistration renders the nodeRegistration block InitConfiguration and JoinConfiguration
// both carry: the name kubeadm registers the node under, and the CRI socket it talks to.
func nodeRegistration(host *cluster.ZarfHost) dig.Mapping {
	return dig.Mapping{
		"name":      host.Hostname,
		"criSocket": containerdSocket,
	}
}

// buildKubeletConfiguration renders kubelet's own component config. cgroupDriver must agree with
// containerd's SystemdCgroup setting in upstream_containerd.go -- a mismatch here leaves kubelet
// refusing to start, with nothing about containerd's own config.toml to say why.
func buildKubeletConfiguration() dig.Mapping {
	return dig.Mapping{
		"apiVersion":   kubeletConfigAPIVersion,
		"kind":         "KubeletConfiguration",
		"cgroupDriver": "systemd",
	}
}

// validateNestedEngineConfig drops any `.spec.config.engine.config` key -- at any depth -- that
// isn't part of kubeadm's ClusterConfiguration for this version, logging each one removed at
// debug, the same drop-and-log contract RancherCommon.validateEngineConfig gives k3s/rke2. If no
// generated schema exists for this version, there's nothing to check against, so it warns once
// and leaves cfg untouched.
func validateNestedEngineConfig(ctx context.Context, version string, cfg dig.Mapping) {
	entry, ok := gen.Lookup(DistroUpstream, version)
	if !ok {
		logger.From(ctx).Warn("no generated engine config schema for this distro/version, skipping config key validation", "distro", DistroUpstream, "version", version)
		return
	}

	node, ok := entry.Server.(extract.FieldNode)
	if !ok {
		return
	}

	dropUnknownNested(ctx, version, cfg, node)
}

// dropUnknownNested recurses into cfg alongside node, deleting any key cfg has that node doesn't
// recognize. A leaf node (Children == nil) stops the recursion there -- its value is a scalar,
// slice, or map, and passes through unvalidated, same as ExtractNestedKeys' extraction stopped
// there.
func dropUnknownNested(ctx context.Context, version string, cfg dig.Mapping, node extract.FieldNode) {
	for k, v := range cfg {
		child, known := node.Children[k]
		if !known {
			logger.From(ctx).Debug("engine config key not recognized for this distro/version, dropping it from the kubeadm config", "distro", DistroUpstream, "version", version, "key", k)
			delete(cfg, k)
			continue
		}
		if child.Children == nil {
			continue
		}
		if nested, ok := v.(dig.Mapping); ok {
			dropUnknownNested(ctx, version, nested, child)
		}
	}
}
