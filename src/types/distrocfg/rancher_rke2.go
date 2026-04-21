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
	DISTRO_ID_RKE2 = "rke2"
)

type RKE2 struct {
	RancherCommon
}

var _ Distro = (*RKE2)(nil)

func init() {
	registry.RegisterDistroModule(
		func(dis string) bool {
			return dis == DISTRO_ID_RKE2
		},
		func() any {
			return &RKE2{
				RancherCommon{
					Common{
						ID:                 DISTRO_ID_RKE2,
						BinaryDir:          "/usr/local/bin",
						Binary:             "rke2",
						Config:             "/etc/rancher/rke2/config.yaml",
						Token:              "/etc/rancher/rke2/token",
						Data:               "/var/lib/rancher/rke2",
						Service_Controller: "rke2-server",
						Service_Worker:     "rke2-agent",
					},
				},
			}
		},
	)
}

// KubeconfigPath implements Distro.
func (d *RKE2) KubeconfigPath(host cluster.ZarfHost, dataDir string) string {
	return filepath.Join(filepath.Dir(d.Config), "rke2.yaml")
}

// KubectlCmdf implements Distro.
func (d *RKE2) KubectlCmdf(host cluster.ZarfHost, dataDir string, s string, args ...any) string {
	return fmt.Sprintf(`env "KUBECONFIG=%s" %s/bin/kubectl %s`, d.KubeconfigPath(host, dataDir), d.DataDirPath(), fmt.Sprintf(s, args...))
}
