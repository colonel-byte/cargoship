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

// ErrNoKubeConfig is returned when the config is asked for before the phase has run
var ErrNoKubeConfig = errors.New("kubeconfig has not been built")

// KubeConfig phase state
type KubeConfig struct {
	GenericPhase
	Distro    distrocfg.Distro
	ClusterID string
	ClusterLB string
	Enabled   bool
	// Write merges the config into the operator's local kubeconfig once it is built. The
	// CLI sets it. A caller that only wants the value -- a provider holding it as an
	// attribute, say -- leaves it false and reads Config after the run, so nobody's
	// ~/.kube/config is touched as a side effect.
	Write bool
	// Path is the kubeconfig file Write merges into. Empty means the standard location:
	// KUBECONFIG when set, otherwise ~/.kube/config. A path that does not exist is created,
	// and one that does keeps every cluster already in it.
	Path         string
	configAccess clientcmd.ConfigAccess
	leader       *cluster.ZarfHost
	built        *clientcmdapi.Config
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
	p.configAccess = p.pathOptions()
	p.ClusterID = c.Metadata.Name
	p.ClusterLB = c.Spec.Config.LoadBalancer
	return nil
}

// pathOptions is where the merge reads from and writes to. Setting ExplicitPath narrows both
// to the one file: it takes precedence over KUBECONFIG and the default path, GetStartingConfig
// returns an empty config rather than an error when the file is not there, and the write
// creates it, and its parent directory, at 0600.
func (p *KubeConfig) pathOptions() clientcmd.ConfigAccess {
	pathOptions := clientcmd.NewDefaultPathOptions()
	if p.Path != "" {
		pathOptions.LoadingRules.ExplicitPath = p.Path
	}
	return pathOptions
}

// ShouldRun is true when enabled by flags
func (p *KubeConfig) ShouldRun() bool {
	return p.Enabled
}

// Run the phase
func (p *KubeConfig) Run(_ context.Context) error {
	creds, err := p.Distro.AdminCredentials(*p.leader, p.Distro.DataDirPath())
	if err != nil {
		return err
	}

	// Built from an empty config rather than from what is on disk, so the value a caller
	// reads carries this cluster and nothing else.
	p.built = p.merge(clientcmdapi.NewConfig(), creds)

	if !p.Write {
		return nil
	}
	return p.writeConfig(creds)
}

// writeConfig merges this cluster into the kubeconfig file at Path -- or at the standard
// location when Path is empty -- and writes it back, leaving every other cluster in the file
// as it was.
func (p *KubeConfig) writeConfig(creds distrocfg.AdminCredentials) error {
	config, err := p.configAccess.GetStartingConfig()
	if err != nil {
		return err
	}
	return clientcmd.ModifyConfig(p.configAccess, *p.merge(config, creds), true)
}

// Config is the kubeconfig for this cluster, built during Run from the admin credentials on
// the leader. It is nil until the phase has run.
func (p *KubeConfig) Config() *clientcmdapi.Config {
	return p.built
}

// Bytes is Config serialized as a kubeconfig file, which is the form a caller storing it
// elsewhere wants.
func (p *KubeConfig) Bytes() ([]byte, error) {
	if p.built == nil {
		return nil, ErrNoKubeConfig
	}
	return clientcmd.Write(*p.built)
}

// merge writes this cluster's entries into config and returns it. An entry that is already
// there is updated in place rather than replaced, so fields cargoship does not own survive,
// and entries for other clusters are left alone. Passing an empty config yields a standalone
// one; passing the operator's yields the merge the CLI writes back.
func (p *KubeConfig) merge(config *clientcmdapi.Config, creds distrocfg.AdminCredentials) *clientcmdapi.Config {
	adminName := fmt.Sprintf("%s-admin", p.ClusterID)

	startingCluster, exists := config.Clusters[p.ClusterID]
	if !exists {
		startingCluster = clientcmdapi.NewCluster()
	}
	cluster := p.modifyCluster(*startingCluster, creds)
	config.Clusters[p.ClusterID] = &cluster

	startingAuth, exists := config.AuthInfos[adminName]
	if !exists {
		startingAuth = clientcmdapi.NewAuthInfo()
	}
	auth := modifyAuthInfo(*startingAuth, creds)
	config.AuthInfos[adminName] = &auth

	config.Contexts[p.ClusterID] = &clientcmdapi.Context{
		Cluster:  p.ClusterID,
		AuthInfo: adminName,
	}

	config.CurrentContext = p.ClusterID

	return config
}

func (p *KubeConfig) modifyCluster(existingCluster clientcmdapi.Cluster, creds distrocfg.AdminCredentials) clientcmdapi.Cluster {
	modifiedCluster := existingCluster

	modifiedCluster.CertificateAuthorityData = creds.CertificateAuthority
	modifiedCluster.Server = fmt.Sprintf("https://%s:6443", p.ClusterLB)

	return modifiedCluster
}

func modifyAuthInfo(existingAuthInfo clientcmdapi.AuthInfo, creds distrocfg.AdminCredentials) clientcmdapi.AuthInfo {
	modifiedAuthInfo := existingAuthInfo

	modifiedAuthInfo.ClientCertificateData = creds.ClientCertificate
	modifiedAuthInfo.ClientKeyData = creds.ClientKey

	return modifiedAuthInfo
}
