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
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/k0sproject/dig"
)

func TestBuildClusterConfigurationDefaults(t *testing.T) {
	dis := distro.ZarfDistro{}
	dis.Spec.Version = "v1.35.3"

	cc := buildClusterConfiguration(dis, cluster.ZarfRuntimeMeta{})

	if cc["kind"] != "ClusterConfiguration" {
		t.Fatalf("kind = %v, want ClusterConfiguration", cc["kind"])
	}
	if cc["kubernetesVersion"] != "v1.35.3" {
		t.Fatalf("kubernetesVersion = %v, want v1.35.3", cc["kubernetesVersion"])
	}
	networking, ok := cc[keyNetworking].(dig.Mapping)
	if !ok {
		t.Fatalf("networking = %v, want a mapping", cc[keyNetworking])
	}
	if networking[keyServiceSubnet] != upstreamServiceCIDR {
		t.Fatalf("serviceSubnet = %v, want %v", networking[keyServiceSubnet], upstreamServiceCIDR)
	}
	if _, ok := networking[keyPodSubnet]; ok {
		t.Fatalf("podSubnet = %v, want absent when unset", networking[keyPodSubnet])
	}
	if _, ok := cc["apiServer"]; ok {
		t.Fatalf("apiServer = %v, want absent when nothing configures it", cc["apiServer"])
	}
}

func TestBuildClusterConfigurationPodSubnetAndCertSANs(t *testing.T) {
	dis := distro.ZarfDistro{}
	dis.Spec.Config.Engine = dig.Mapping{
		"config": dig.Mapping{
			keyNetworking: dig.Mapping{keyPodSubnet: "10.10.0.0/16"},
		},
	}
	run := cluster.ZarfRuntimeMeta{ControllerTLS: []string{"cluster.example.com"}}

	cc := buildClusterConfiguration(dis, run)

	networking, ok := cc[keyNetworking].(dig.Mapping)
	if !ok {
		t.Fatalf("networking = %v, want a mapping", cc[keyNetworking])
	}
	if networking[keyPodSubnet] != "10.10.0.0/16" {
		t.Fatalf("podSubnet = %v, want 10.10.0.0/16", networking[keyPodSubnet])
	}
	apiServer, ok := cc["apiServer"].(dig.Mapping)
	if !ok {
		t.Fatalf("apiServer = %v, want a mapping with certSANs", cc["apiServer"])
	}
	sans, ok := apiServer["certSANs"].([]string)
	if !ok {
		t.Fatalf("certSANs = %v, want a []string", apiServer["certSANs"])
	}
	if len(sans) != 1 || sans[0] != "cluster.example.com" {
		t.Fatalf("certSANs = %v, want [cluster.example.com]", sans)
	}
}

func TestBuildClusterConfigurationAuditAndPSSMountHostPaths(t *testing.T) {
	dis := distro.ZarfDistro{}
	dis.Spec.Config.Engine = dig.Mapping{
		"audit":       dig.Mapping{"rules": []any{dig.Mapping{"level": "Metadata"}}},
		"podSecurity": dig.Mapping{"defaults": dig.Mapping{"enforce": "restricted"}},
	}

	cc := buildClusterConfiguration(dis, cluster.ZarfRuntimeMeta{})

	apiServer, ok := cc["apiServer"].(dig.Mapping)
	if !ok {
		t.Fatalf("apiServer = %v, want a mapping", cc["apiServer"])
	}
	vols, ok := apiServer["extraVolumes"].([]dig.Mapping)
	if !ok || len(vols) != 2 {
		t.Fatalf("extraVolumes = %v, want 2 hostPath mounts", apiServer["extraVolumes"])
	}
	args, ok := apiServer["extraArgs"].([]any)
	if !ok {
		t.Fatalf("extraArgs = %v, want a list", apiServer["extraArgs"])
	}
	var flags []string
	for _, a := range args {
		m := a.(dig.Mapping)                      //nolint:errcheck
		flags = append(flags, m["name"].(string)) //nolint:errcheck
	}
	want := []string{"audit-policy-file", "audit-log-path", "admission-control-config-file"}
	if strings.Join(flags, ",") != strings.Join(want, ",") {
		t.Fatalf("extraArgs names = %v, want %v", flags, want)
	}
}

func TestBuildClusterConfigurationExtraArgsPassthrough(t *testing.T) {
	dis := distro.ZarfDistro{}
	dis.Spec.Config.Engine = dig.Mapping{
		"config": dig.Mapping{
			"controllerManager": dig.Mapping{
				"extraArgs": []any{extraArg("cluster-signing-key-file", "/etc/kubernetes/ca.key")},
			},
		},
	}

	cc := buildClusterConfiguration(dis, cluster.ZarfRuntimeMeta{})

	cm, ok := cc["controllerManager"].(dig.Mapping)
	if !ok {
		t.Fatalf("controllerManager = %v, want a mapping", cc["controllerManager"])
	}
	args, ok := cm["extraArgs"].([]any)
	if !ok {
		t.Fatalf("extraArgs = %v, want a []any", cm["extraArgs"])
	}
	if len(args) != 1 {
		t.Fatalf("extraArgs = %v, want 1 entry", args)
	}
}

func TestBuildClusterConfigurationImageRepository(t *testing.T) {
	dis := distro.ZarfDistro{}
	dis.Spec.Config.ImagesConfig.Images = []string{"registry.k8s.io/kube-apiserver:v1.35.3"}

	cc := buildClusterConfiguration(dis, cluster.ZarfRuntimeMeta{})

	if cc["imageRepository"] != "registry.k8s.io" {
		t.Fatalf("imageRepository = %v, want registry.k8s.io", cc["imageRepository"])
	}
}

func TestBuildInitConfiguration(t *testing.T) {
	host := &cluster.ZarfHost{Hostname: "controller-1"}

	ic := buildInitConfiguration(host)

	if ic["kind"] != "InitConfiguration" {
		t.Fatalf("kind = %v, want InitConfiguration", ic["kind"])
	}
	nr, ok := ic["nodeRegistration"].(dig.Mapping)
	if !ok || nr["name"] != "controller-1" || nr["criSocket"] != containerdSocket {
		t.Fatalf("nodeRegistration = %v, want name controller-1 and criSocket %v", nr, containerdSocket)
	}
	if _, ok := ic["bootstrapTokens"]; ok {
		t.Fatalf("bootstrapTokens = %v, want absent", ic["bootstrapTokens"])
	}
}

func TestBuildJoinConfigurationWorker(t *testing.T) {
	host := &cluster.ZarfHost{Hostname: "worker-1", Role: cluster.RoleWorker}
	run := cluster.ZarfRuntimeMeta{AgentToken: "agent-token", LoadBalancer: "lb.example.com"}

	jc := buildJoinConfiguration(host, run)

	if jc["kind"] != "JoinConfiguration" {
		t.Fatalf("kind = %v, want JoinConfiguration", jc["kind"])
	}
	discovery, ok := jc["discovery"].(dig.Mapping)
	if !ok {
		t.Fatalf("discovery = %v, want a mapping", jc["discovery"])
	}
	bt, ok := discovery["bootstrapToken"].(dig.Mapping)
	if !ok || bt["token"] != "agent-token" || bt["apiServerEndpoint"] != "lb.example.com" {
		t.Fatalf("bootstrapToken = %v, want agent-token/lb.example.com", bt)
	}
	if _, ok := jc["controlPlane"]; ok {
		t.Fatalf("controlPlane = %v, want absent for a worker", jc["controlPlane"])
	}
}

func TestBuildJoinConfigurationController(t *testing.T) {
	host := &cluster.ZarfHost{Hostname: "controller-2", Role: cluster.RoleController}
	run := cluster.ZarfRuntimeMeta{ControllerToken: "controller-token"}

	jc := buildJoinConfiguration(host, run)

	discovery, ok := jc["discovery"].(dig.Mapping)
	if !ok {
		t.Fatalf("discovery = %v, want a mapping", jc["discovery"])
	}
	bt, ok := discovery["bootstrapToken"].(dig.Mapping)
	if !ok {
		t.Fatalf("bootstrapToken = %v, want a mapping", discovery["bootstrapToken"])
	}
	if bt["token"] != "controller-token" {
		t.Fatalf("bootstrapToken.token = %v, want controller-token", bt["token"])
	}
	if _, ok := jc["controlPlane"]; !ok {
		t.Fatalf("controlPlane = %v, want present for a joining controller", jc["controlPlane"])
	}
}

func TestBuildKubeletConfiguration(t *testing.T) {
	kc := buildKubeletConfiguration()

	if kc["kind"] != "KubeletConfiguration" || kc["cgroupDriver"] != "systemd" {
		t.Fatalf("KubeletConfiguration = %v, want cgroupDriver systemd", kc)
	}
}

func TestMarshalYAMLDocsConcatenatesDocuments(t *testing.T) {
	b, err := marshalYAMLDocs(dig.Mapping{"kind": "A"}, dig.Mapping{"kind": "B"})
	if err != nil {
		t.Fatalf("marshalYAMLDocs() error = %v, want nil", err)
	}

	out := string(b)
	if strings.Count(out, "---") != 2 {
		t.Fatalf("marshalYAMLDocs() = %q, want 2 document separators", out)
	}
	if !strings.Contains(out, "kind: A") || !strings.Contains(out, "kind: B") {
		t.Fatalf("marshalYAMLDocs() = %q, want both documents", out)
	}
}
