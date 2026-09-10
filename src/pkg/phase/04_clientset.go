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

package phase

import (
	"fmt"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/types/distrocfg"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// apiServerPort is the port every supported distro serves the Kubernetes API on.
const apiServerPort = 6443

// distroClientset builds a Kubernetes client from a leader's admin certificates, addressed at the
// cluster load balancer. The certificates are read off the leader over SSH each time rather than
// cached, so the client is only as long-lived as the phase that asked for it.
func distroClientset(d distrocfg.Distro, leader cluster.ZarfHost, clusterLB string) (*kubernetes.Clientset, error) {
	creds, err := d.AdminCredentials(leader, d.DataDirPath())
	if err != nil {
		return nil, fmt.Errorf("failed to read admin credentials: %w", err)
	}

	return kubernetes.NewForConfig(&rest.Config{
		Host: fmt.Sprintf("https://%s:%d", clusterLB, apiServerPort),
		TLSClientConfig: rest.TLSClientConfig{
			CAData:   creds.CertificateAuthority,
			CertData: creds.ClientCertificate,
			KeyData:  creds.ClientKey,
		},
	})
}
