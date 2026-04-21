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
	"fmt"
	"path/filepath"

	"github.com/colonel-byte/mare/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/mare/src/types/distrocfg/registry"
)

const (
	DISTRO_ID_K3S = "k3s"
)

type K3S struct {
	RancherCommon
}

var _ Distro = (*K3S)(nil)

func init() {
	registry.RegisterDistroModule(
		func(dis string) bool {
			return dis == DISTRO_ID_K3S
		},
		func() any {
			return &K3S{
				RancherCommon{
					Common{
						ID:                 DISTRO_ID_K3S,
						BinaryDir:          "/usr/local/bin",
						Binary:             "k3s",
						Config:             "/etc/rancher/k3s/config.yaml",
						Token:              "/etc/rancher/k3s/token",
						Data:               "/var/lib/rancher/k3s",
						Service_Controller: "k3s-server",
						Service_Worker:     "k3s-agent",
					},
				},
			}
		},
	)
}

// KubeconfigPath implements Distro.
func (d *K3S) KubeconfigPath(host cluster.ZarfHost, dataDir string) string {
	return filepath.Join(filepath.Dir(d.Config), "k3s.yaml")
}

// KubectlCmdf implements Distro.
func (d *K3S) KubectlCmdf(host cluster.ZarfHost, dataDir string, s string, args ...any) string {
	return fmt.Sprintf(`env "KUBECONFIG=%s" %s`, d.KubeconfigPath(host, dataDir), d.DistroCmdf(`kubectl %s`, fmt.Sprintf(s, args...)))
}
