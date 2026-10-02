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
	"encoding/base64"
	"path/filepath"
	"strings"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/k0sproject/dig"
)

const (
	// containerdConfigPath is where containerd reads its own configuration from.
	containerdConfigPath = "/etc/containerd/config.toml"
	// containerdCertsDir is where containerd looks for a per-registry hosts.toml, when told to
	// with the registry.config_path key below. Without that key set, containerd never reads any
	// hosts.toml at all, no matter what is written under this directory.
	containerdCertsDir = "/etc/containerd/certs.d"
	// crictlConfigPath is where crictl reads its own configuration from.
	crictlConfigPath = "/etc/crictl.yaml"
	// containerdSocket is containerd's default CRI socket, used by both crictl and kubelet.
	containerdSocket = "unix:///run/containerd/containerd.sock"
)

// sandboxImage returns the packaged pause image, if one is bundled. Nothing here invents a
// version: containerd's own default sandbox image is left in place when the package does not
// carry one, rather than guessing at a tag that might not match what was actually uploaded.
func sandboxImage(images []string) string {
	for _, ref := range images {
		name := ref
		if idx := strings.LastIndex(name, "/"); idx != -1 {
			name = name[idx+1:]
		}
		if name == "pause" || strings.HasPrefix(name, "pause:") || strings.HasPrefix(name, "pause@") {
			return ref
		}
	}
	return ""
}

// buildContainerdConfig renders containerd's config.toml: the systemd cgroup driver kubelet
// expects, the registry.config_path that makes containerd read any hosts.toml at all, and the
// packaged pause image when one is bundled.
func buildContainerdConfig(dis distro.ZarfDistro) dig.Mapping {
	cri := dig.Mapping{
		"containerd": dig.Mapping{
			"runtimes": dig.Mapping{
				"runc": dig.Mapping{
					"options": dig.Mapping{
						"SystemdCgroup": true,
					},
				},
			},
		},
		"registry": dig.Mapping{
			"config_path": containerdCertsDir,
		},
	}
	if img := sandboxImage(dis.Spec.Config.ImagesConfig.Images); img != "" {
		cri["sandbox_image"] = img
	}

	return dig.Mapping{
		"version": 2,
		"plugins": dig.Mapping{
			"io.containerd.grpc.v1.cri": cri,
		},
	}
}

// buildCrictlConfig renders crictl.yaml, pointing both the runtime and image endpoints at
// containerd's own socket.
func buildCrictlConfig() dig.Mapping {
	return dig.Mapping{
		"runtime-endpoint": containerdSocket,
		"image-endpoint":   containerdSocket,
	}
}

// registryAuthHeader renders one registry's credentials as an HTTP Authorization header value,
// for containerd's hosts.toml [host.*.header] table. Containerd has no separate identity_token
// concept the way wharfie's registries.yaml does -- a bearer token and a basic auth pair are both
// just an Authorization header from its point of view. A username or password given alone cannot
// form a valid header, so it is left out rather than sent malformed.
func registryAuthHeader(a cluster.ZarfClusterRegistryAuth) string {
	switch {
	case a.Token != "":
		return "Bearer " + a.Token
	case a.Username != "" && a.Password != "":
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(a.Username+":"+a.Password))
	default:
		return ""
	}
}

// buildContainerdHostsConfigs builds one hosts.toml document per registry cargoship configures a
// mirror, credential, or TLS setting for, keyed by the registry's own name -- the origin
// containerd looks up a hosts.toml directory by, which is not the same host ConfigHost answers:
// that is which host the credentials/TLS settings belong to (the mirror when there is one), while
// this key is always the registry being redirected away from, mirror or not.
//
// CA and client certificate paths are reused as written by registryCAFiles/registryTLS for
// wharfie's registries.yaml -- that logic is already format-agnostic (a path on disk), only the
// document shape around it differs here.
func buildContainerdHostsConfigs(registries []cluster.ZarfClusterRegistries) map[string]dig.Mapping {
	docs := map[string]dig.Mapping{}

	for _, reg := range registries {
		endpoint := reg.MirrorEndpoint()
		if endpoint == "" {
			endpoint = "https://" + string(reg.Name)
		}

		hostConfig := dig.Mapping{}
		if header := registryAuthHeader(reg.Authentication); header != "" {
			hostConfig["header"] = dig.Mapping{"Authorization": header}
		}
		if t := reg.TLS; t != nil {
			switch {
			case t.CA != "":
				hostConfig["ca"] = registryCAPath(reg.ConfigHost())
			case t.CAFile != "":
				hostConfig["ca"] = t.CAFile
			}
			if t.CertFile != "" && t.KeyFile != "" {
				hostConfig["client"] = [][]string{{t.CertFile, t.KeyFile}}
			}
			if t.InsecureSkipVerify {
				hostConfig["skip_verify"] = true
			}
		}

		if len(hostConfig) == 0 && reg.MirrorEndpoint() == "" {
			continue
		}

		docs[string(reg.Name)] = dig.Mapping{
			"server": "https://" + string(reg.Name),
			"host": dig.Mapping{
				endpoint: hostConfig,
			},
		}
	}

	return docs
}

// containerdHostsPath returns where a registry's hosts.toml is written, keyed by its own name
// rather than by ConfigHost -- containerd looks a hosts.toml up by the registry it was asked to
// pull from, before it knows anything about a mirror.
func containerdHostsPath(name string) string {
	return filepath.Join(containerdCertsDir, name, "hosts.toml")
}
