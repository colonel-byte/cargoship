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

// This file holds what the kube-vip flavors pin, and the one block of template data that
// follows from it. The flavors themselves are declared in distros.go, with the rest.

package examples

import "fmt"

const (
	// kubeVipVersion is the kube-vip release the kube-vip flavors carry.
	//
	// Moving it is a hand edit here followed by mage generate:examples, the way
	// upstreamFlannelVersion and upstreamCanalVersion are moved. Neither Dependabot nor a
	// mage target owns it: the value is a Go constant in a file no updater reads, and the
	// image it names is resolved from the registry when a package is built rather than
	// hashed here, so there is no checksum to re-derive either. What to check when moving it:
	//
	//   - the env var names in example/kube-vip/kube-vip.yaml are still the ones
	//     upstream kube-vip's own pkg/kubevip/config_envvar.go defines for the new release. They are the pod's whole
	//     configuration surface, so a renamed one is a pod that starts and advertises
	//     nothing, with no error anybody sees.
	//   - the release still publishes a linux/amd64 image under kubeVipImageRepo.
	//
	// https://github.com/kube-vip/kube-vip/releases
	kubeVipVersion = "v1.3.1"

	// kubeVipImageRepo is where kube-vip publishes its image. Pinned by tag rather than by
	// digest, like every other image an example carries: a package resolves and records the
	// digest when it is built, so the tag here is re-resolved rather than trusted.
	kubeVipImageRepo = "ghcr.io/kube-vip/kube-vip"

	// kubeVipManifestSource is the static pod, relative to the directory holding a rendered
	// distro.yaml -- <flavor>/<minor>/<version>, so three levels up. It sits beside the
	// flavor directories rather than inside one because the flavors of both distros share
	// it; see the file's own header for why it is one file and not one per release.
	kubeVipManifestSource = "../../../kube-vip/kube-vip.yaml"

	// kubeVipPodManifestDir is where an engine's kubelet reads static pods from, under the
	// distro's data directory.
	//
	// k3s and rke2 agree on it -- k3s joins DefaultPodManifestPath onto <data-dir>/agent in
	// pkg/agent/config/config.go, and rke2 runs the same agent -- which is what lets one
	// manifest serve both flavors. It is deliberately not the engine's auto-deploy manifest
	// directory: a static pod dropped there would be applied through the API server, which
	// cannot work for the pod whose job is to bring up the address the API server is
	// reached on.
	kubeVipPodManifestDir = "agent/pod-manifests"

	// kubeVipSubnet is the prefix length kube-vip adds the VIP with. A /32 is the single
	// address and nothing else, which is what a VIP moving between controllers wants: a
	// wider prefix would have kube-vip claim a route for a subnet the host already has one
	// for.
	kubeVipSubnet = "32"

	// kubeVipMetricsAddress is what kube-vip serves its Prometheus endpoint on. Loopback
	// rather than every interface, because the pod is on the host network: binding 0.0.0.0
	// would publish an unauthenticated endpoint on every address the controller has, the
	// VIP included.
	kubeVipMetricsAddress = "127.0.0.1:2112"
)

// exampleKubeVip is what a kube-vip flavor's templates render from. It is nil on every other
// flavor, so one `{{ with .KubeVip }}` in each distro template covers the whole feature.
//
// The two fields that are not shared are the two that follow from the engine rather than from
// kube-vip: where its kubelet reads static pods, and where the engine writes the admin
// kubeconfig kube-vip holds its lease with. Each distro's derive hook fills them in.
type exampleKubeVip struct {
	// Image is the reference the package carries and the static pod runs.
	Image string
	// Source is the static pod template the package stages.
	Source string
	// Target is where the static pod lands on a controller.
	Target string
	// Kubeconfig is the engine's admin kubeconfig on the host, which the pod mounts.
	Kubeconfig string
	// Address is the address override the flavor's values file ships, empty so the
	// inventory's own loadbalancer is what the pod advertises. See the values file.
	//
	// Subnet, MetricsAddress are the defaults the flavor's values file ships.
	Subnet         string
	MetricsAddress string
	// Port is the API server port kube-vip health-checks the control plane on.
	Port string
}

// newExampleKubeVip is the kube-vip block for a distro whose data directory is dataDir, whose
// admin kubeconfig is at kubeconfig, and whose API server listens on port.
func newExampleKubeVip(dataDir, kubeconfig, port string) *exampleKubeVip {
	return &exampleKubeVip{
		Image:          fmt.Sprintf("%s:%s", kubeVipImageRepo, kubeVipVersion),
		Source:         kubeVipManifestSource,
		Target:         fmt.Sprintf("%s/%s/kube-vip.yaml", dataDir, kubeVipPodManifestDir),
		Kubeconfig:     kubeconfig,
		Subnet:         kubeVipSubnet,
		MetricsAddress: kubeVipMetricsAddress,
		Port:           port,
	}
}
