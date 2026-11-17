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

package examples

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/pkg/helmvalues"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// kubeVipManifestPath is the shared static pod, from this package's directory.
const kubeVipManifestPath = repoRoot + "/example/kube-vip/kube-vip.yaml"

// TestKubeVipManifestRenders renders the shared static pod the way an install does -- against
// the flavor's own values and the cluster facts cargoship resolves -- and reads the result back
// as YAML.
//
// Nothing else checks this file. It is not a generated example, so no render covers it; it is a
// template, so no YAML parser in the toolchain can read it as committed; and the one thing it
// has to get right is a Kubernetes manifest whose only reader is a kubelet on an air-gapped
// controller. A missing quote here is a static pod the kubelet rejects in a log nobody is
// watching while the cluster fails to form.
func TestKubeVipManifestRenders(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean(kubeVipManifestPath))
	require.NoError(t, err)

	values := map[string]any{
		"kubeVip": map[string]any{
			"enabled":        true,
			"image":          "ghcr.io/kube-vip/kube-vip:v0.0.0",
			"interface":      "",
			"subnet":         "32",
			"port":           "6443",
			"kubeconfig":     "/etc/rancher/rke2/rke2.yaml",
			"leaseDuration":  "5",
			"renewDeadline":  "3",
			"retryPeriod":    "1",
			"metricsAddress": "127.0.0.1:2112",
		},
	}
	data := map[string]any{
		"Values":  values,
		"Cluster": map[string]any{"LoadBalancer": "kube.cargoship.test"},
	}

	out, err := helmvalues.RenderTemplate(string(raw), data)
	require.NoError(t, err)

	var pod struct {
		Kind string `yaml:"kind"`
		Spec struct {
			HostNetwork bool `yaml:"hostNetwork"`
			Containers  []struct {
				Image string `yaml:"image"`
				Env   []struct {
					Name  string `yaml:"name"`
					Value string `yaml:"value"`
				} `yaml:"env"`
			} `yaml:"containers"`
			Volumes []struct {
				HostPath struct {
					Path string `yaml:"path"`
				} `yaml:"hostPath"`
			} `yaml:"volumes"`
		} `yaml:"spec"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(out), &pod), "the rendered static pod does not parse")

	require.Equal(t, "Pod", pod.Kind)
	require.True(t, pod.Spec.HostNetwork, "the VIP is an address on the host's own namespace")
	require.Len(t, pod.Spec.Containers, 1)
	require.Equal(t, "ghcr.io/kube-vip/kube-vip:v0.0.0", pod.Spec.Containers[0].Image)
	require.Equal(t, "/etc/rancher/rke2/rke2.yaml", pod.Spec.Volumes[0].HostPath.Path)

	env := map[string]string{}
	for _, e := range pod.Spec.Containers[0].Env {
		env[e.Name] = e.Value
	}
	// The address is the one assertion worth making by name: it is the whole reason the
	// manifest reads a cluster fact rather than a value, and a blank one is a pod that runs
	// and advertises nothing.
	require.Equal(t, "kube.cargoship.test", env["address"], "the VIP comes from the inventory's loadbalancer")
	require.Equal(t, "true", env["cp_enable"], "this is a control-plane VIP")
	require.Equal(t, "false", env["svc_enable"], "the package carries no address pool for Services")
}

// A package with kube-vip switched off is still rendered -- the `when` gate decides whether it
// is uploaded, not whether it is rendered -- so the manifest has to render without a cluster
// loadbalancer. Otherwise an inventory that never asked for kube-vip could not install the
// package at all.
func TestKubeVipManifestRendersDisabledWithoutALoadBalancer(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean(kubeVipManifestPath))
	require.NoError(t, err)

	data := map[string]any{
		"Values":  map[string]any{"kubeVip": map[string]any{"enabled": false}},
		"Cluster": map[string]any{},
	}

	out, err := helmvalues.RenderTemplate(string(raw), data)
	require.NoError(t, err)

	var pod map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(out), &pod))
	require.Empty(t, pod, "a disabled kube-vip renders to comments and nothing else")
}

// The flavors and the static pod agree on the values the pod reads: the flavor's values file is
// where every one of them gets its default, so a key the manifest interpolates and the values
// file does not set is a render that fails on an operator's management node.
func TestKubeVipValuesCoverTheManifest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean(kubeVipManifestPath))
	require.NoError(t, err)

	for _, spec := range exampleDistros {
		for _, f := range spec.flavors {
			if !f.kubeVip {
				continue
			}
			t.Run(spec.name, func(t *testing.T) {
				data := map[string]any{
					"Name":    f.flavorName(),
					"CNI":     f.cni,
					"Minor":   "v1.35",
					"Addons":  []string{"rke2-coredns"},
					"KubeVip": spec.kubeVip,
				}

				values := map[string]any{}
				for _, path := range f.values {
					if filepath.Ext(path) != ".tmpl" || filepath.Base(path) != "values.yaml.tmpl" {
						continue
					}
					rendered := renderValuesTemplate(t, filepath.Join(repoRoot, path), data)
					require.NoError(t, yaml.Unmarshal([]byte(rendered), &values))
				}
				// Enabled, because the guarded body is the part that reads values.
				kubeVip, ok := values["kubeVip"].(map[string]any)
				require.True(t, ok, "the flavor's values file carries no kubeVip block")
				kubeVip["enabled"] = true

				out, err := helmvalues.RenderTemplate(string(raw), map[string]any{
					"Values":  values,
					"Cluster": map[string]any{"LoadBalancer": "kube.cargoship.test"},
				})
				require.NoError(t, err, "the static pod reads a value this flavor does not set")

				var pod struct {
					Spec struct {
						Volumes []struct {
							HostPath struct {
								Path string `yaml:"path"`
							} `yaml:"hostPath"`
						} `yaml:"volumes"`
					} `yaml:"spec"`
				}
				require.NoError(t, yaml.Unmarshal([]byte(out), &pod))
				require.Len(t, pod.Spec.Volumes, 1)
				require.Equal(t, spec.kubeVip.Kubeconfig, pod.Spec.Volumes[0].HostPath.Path,
					"the pod mounts this distro's own kubeconfig")
			})
		}
	}
}

// A load balancer is a *hostname* in the cluster schema, and that is how most fleets set it --
// but kube-vip has to resolve a name on the controller before the kubelet starts this pod, and
// cargoship puts nothing in /etc/hosts for it. So the address is overridable, and the override
// is what an air-gapped fleet with an FQDN endpoint and no DNS for it uses.
func TestKubeVipManifestAddressOverride(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean(kubeVipManifestPath))
	require.NoError(t, err)

	render := func(t *testing.T, address, loadBalancer string) map[string]string {
		t.Helper()
		values := map[string]any{
			"kubeVip": map[string]any{
				"enabled":        true,
				"address":        address,
				"image":          "ghcr.io/kube-vip/kube-vip:v0.0.0",
				"interface":      "",
				"subnet":         "32",
				"port":           "6443",
				"kubeconfig":     "/etc/rancher/rke2/rke2.yaml",
				"leaseDuration":  "5",
				"renewDeadline":  "3",
				"retryPeriod":    "1",
				"metricsAddress": "127.0.0.1:2112",
			},
		}
		cluster := map[string]any{}
		if loadBalancer != "" {
			cluster["LoadBalancer"] = loadBalancer
		}

		out, err := helmvalues.RenderTemplate(string(raw), map[string]any{"Values": values, "Cluster": cluster})
		require.NoError(t, err)

		var pod struct {
			Spec struct {
				Containers []struct {
					Env []struct {
						Name  string `yaml:"name"`
						Value string `yaml:"value"`
					} `yaml:"env"`
				} `yaml:"containers"`
			} `yaml:"spec"`
		}
		require.NoError(t, yaml.Unmarshal([]byte(out), &pod))
		env := map[string]string{}
		for _, e := range pod.Spec.Containers[0].Env {
			env[e.Name] = e.Value
		}
		return env
	}

	t.Run("no override takes the inventory's loadbalancer", func(t *testing.T) {
		require.Equal(t, "kube.cargoship.test", render(t, "", "kube.cargoship.test")["address"])
	})

	t.Run("an override wins over it", func(t *testing.T) {
		require.Equal(t, "10.0.0.50", render(t, "10.0.0.50", "kube.cargoship.test")["address"])
	})

	// The one case that has to stay an error: neither set. A pod advertising nothing is a
	// cluster that bootstraps and then cannot be joined.
	t.Run("neither is an error", func(t *testing.T) {
		values := map[string]any{"kubeVip": map[string]any{"enabled": true, "address": ""}}
		_, err := helmvalues.RenderTemplate(string(raw), map[string]any{
			"Values":  values,
			"Cluster": map[string]any{},
		})
		require.Error(t, err)
	})
}
