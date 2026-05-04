// Copyright 2023 k0sctl authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from k0sctl:
// https://github.com/k0sproject/k0sctl
//
// Modifications Copyright 2026 colonel-byte.
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
	"context"
	"errors"
	"fmt"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/types/distrocfg"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// ErrNoControllers an error for when no controllers are running
var ErrNoControllers = errors.New("no controllers are running")

// KubeConfig phase state
type KubeConfig struct {
	GenericPhase
	Distro       distrocfg.Distro
	ClusterID    string
	ClusterLB    string
	Enabled      bool
	configAccess clientcmd.ConfigAccess
	leader       *cluster.ZarfHost
}

// Title for the phase
func (p *KubeConfig) Title() string {
	return "Updating kubeconfig file with the current cluster"
}

// Explanation about the current phase, used for documentation generation
func (p *KubeConfig) Explanation() string {
	return "If enabled, this will update the local kubeconfig with the admin creds for the current distro"
}

// Prepare the phase
func (p *KubeConfig) Prepare(ctx context.Context, c *cluster.ZarfCluster, _ *distro.ZarfDistro) error {
	control := p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return h.Configurer.ServiceIsRunning(h, p.Distro.GetControllerService()) && h.IsController()
	})
	if len(control) > 0 {
		p.leader = control[0]
	} else {
		logger.From(ctx).Warn("there is no running controllers")
		return ErrNoControllers
	}
	p.configAccess = clientcmd.NewDefaultPathOptions()
	p.ClusterID = c.Metadata.Name
	p.ClusterLB = c.Spec.Config.LoadBalancer
	return nil
}

// ShouldRun is true when enabled by flags
func (p *KubeConfig) ShouldRun() bool {
	return p.Enabled
}

// Run the phase
func (p *KubeConfig) Run(_ context.Context) error {
	pathOptions := clientcmd.NewDefaultPathOptions()
	config, err := pathOptions.GetStartingConfig()
	if err != nil {
		return err
	}
	startingCluster, exists := config.Clusters[p.ClusterID]
	if !exists {
		startingCluster = clientcmdapi.NewCluster()
	}
	cluster := p.modifyCluster(*startingCluster)
	config.Clusters[p.ClusterID] = &cluster

	startingAuth, exists := config.AuthInfos[fmt.Sprintf("%s-admin", p.ClusterID)]
	if !exists {
		startingAuth = clientcmdapi.NewAuthInfo()
	}
	auth := p.modifyAuthInfo(*startingAuth)
	config.AuthInfos[fmt.Sprintf("%s-admin", p.ClusterID)] = &auth

	config.Contexts[p.ClusterID] = &clientcmdapi.Context{
		Cluster:  p.ClusterID,
		AuthInfo: fmt.Sprintf("%s-admin", p.ClusterID),
	}

	config.CurrentContext = p.ClusterID

	return clientcmd.ModifyConfig(pathOptions, *config, true)
}

func (p *KubeConfig) modifyCluster(existingCluster clientcmdapi.Cluster) clientcmdapi.Cluster {
	modifiedCluster := existingCluster

	ca, err := p.leader.ReadFile("/var/lib/rancher/rke2/server/tls/server-ca.crt")
	if err != nil {
		return modifiedCluster
	}
	modifiedCluster.CertificateAuthorityData = []byte(ca)
	modifiedCluster.Server = fmt.Sprintf("https://%s:6443", p.ClusterLB)

	return modifiedCluster
}

func (p *KubeConfig) modifyAuthInfo(existingAuthInfo clientcmdapi.AuthInfo) clientcmdapi.AuthInfo {
	modifiedAuthInfo := existingAuthInfo

	crt, err := p.leader.ReadFile("/var/lib/rancher/rke2/server/tls/client-admin.crt")
	if err != nil {
		return modifiedAuthInfo
	}
	key, err := p.leader.ReadFile("/var/lib/rancher/rke2/server/tls/client-admin.key")
	if err != nil {
		return modifiedAuthInfo
	}

	modifiedAuthInfo.ClientCertificateData = []byte(crt)
	modifiedAuthInfo.ClientKeyData = []byte(key)

	return modifiedAuthInfo
}
