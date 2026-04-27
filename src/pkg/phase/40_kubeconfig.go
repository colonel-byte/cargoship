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
	"context"
	"errors"

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
	return nil
}

// Run the phase
func (p *KubeConfig) Run(_ context.Context) error {
	pathOptions := clientcmd.NewDefaultPathOptions()
	config, err := pathOptions.GetStartingConfig()
	if err != nil {
		return err
	}
	startingStanza, exists := config.Clusters[p.ClusterID]
	if !exists {
		startingStanza = clientcmdapi.NewCluster()
	}
	cluster := p.modifyCluster(*startingStanza)
	config.Clusters[p.ClusterID] = &cluster

	return clientcmd.ModifyConfig(pathOptions, *config, true)
}

func (p *KubeConfig) modifyCluster(existingCluster clientcmdapi.Cluster) clientcmdapi.Cluster {
	modifiedCluster := existingCluster

	return modifiedCluster
}
