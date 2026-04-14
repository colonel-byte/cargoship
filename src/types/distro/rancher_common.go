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

package distro

import (
	"context"

	"github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/zarf-distro/src/config"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type RancherCommon struct {
	Common
}

// Both RKE2 and k3s share similar logic on how to configure the Kubernetes Engine.
// config.yaml:
// we look at `.spec.config.engine.config`, to determine the the globally shared config across all nodes
// audit-config.yaml:
// we look at `.spec.config.engine.audit`, to determine the kubelet audit settings. There is no validation done at this time, please reference: https://kubernetes.io/docs/reference/config-api/apiserver-audit.v1/#audit-k8s-io-v1-Policy
// if `.spec.config.engine.audit` is present we will add/overwrite "audit-policy-file" with the value of "/etc/rancher/rke2/audit-config.yaml"
// rke2-pss.yaml:
// we look at `.spec.config.engine.podSecurity`, to determine the "pod security admission" that will be enforced by the kubelet. There is no validation done at this time, please reference: https://kubernetes.io/docs/concepts/security/pod-security-admission/
// if `.spec.config.engine.podSecurity` is present we will add/overwrite "pod-security-admission-config-file" with the value of "/etc/rancher/rke2/rke2-pss.yaml"

// ConfigureEngine implements Distro.
func (r *RancherCommon) ConfigureEngine(ctx context.Context, host cluster.ZarfHost, dis distro.ZarfDistro) error {
	newBaseConfig := dis.Spec.Config.Engine.Dup()

	logger.From(ctx).Warn("test", newBaseConfig.Dig(config.EngineConfig))

	// err := host.WriteFile(r.Config+".test", test, "0600")
	// if err != nil {
	// 	return err
	// }

	return nil
}
