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

package action

import (
	"context"
	"fmt"
	"time"

	"github.com/colonel-byte/cargoship/pkg/phase"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/colonel-byte/cargoship/types/distrocfg/registry"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// KubeConfigOptions struct
type KubeConfigOptions struct {
	// Manager is the phase manager
	Manager *phase.Manager
	// KubeConfigPath is the kubeconfig file to merge the admin creds into, the standard
	// location when empty
	KubeConfigPath string
	// NoWrite leaves the operator's kubeconfig alone, so the credentials are built and returned
	// and nothing on this machine is touched.
	//
	// It is phrased as the negative so that the zero value writes, which is what every caller
	// before this field existed did: `cargoship install kube-config` exists to write the file.
	// A caller that wants the value instead reads Bytes or Config after Run; see
	// docs/agent/choice-tofu-secrets.md for why the OpenTofu provider is that caller and why it
	// does not expose the result unless it is asked to.
	NoWrite bool
}

// KubeConfig state logic
type KubeConfig struct {
	KubeConfigOptions
	Phases phase.Phases
	// kubeconfig is the phase that builds the credentials, retained so Bytes and Config can read
	// what it built.
	kubeconfig *phase.KubeConfig
}

// NewKubeConfig pulls the admin cert from a control-plane node and updates the local kube-config
func NewKubeConfig(opts KubeConfigOptions) (*KubeConfig, error) {
	disBuilder, err := registry.GetDistroModuleBuilder(opts.Manager.DistroID)
	if err != nil {
		return nil, fmt.Errorf("no distro module for %q: %w", opts.Manager.DistroID, err)
	}

	d, ok := disBuilder().(distrocfg.Distro)
	if !ok {
		return nil, fmt.Errorf("the distro module for %q does not implement the distro interface", opts.Manager.DistroID)
	}

	kubeconfig := &phase.KubeConfig{
		Distro:    d,
		ClusterID: opts.Manager.Config.Metadata.Name,
		Enabled:   true,
		Write:     !opts.NoWrite,
		Path:      opts.KubeConfigPath,
	}

	lockPhase := &phase.Lock{}
	return &KubeConfig{
		KubeConfigOptions: opts,
		kubeconfig:        kubeconfig,
		Phases: phase.Phases{
			&phase.Connect{},
			&phase.DetectOS{},
			lockPhase,
			&phase.GatherFacts{},
			kubeconfig,
			lockPhase.UnlockPhase(),
			&phase.Disconnect{},
		},
	}, nil
}

// Bytes is the kubeconfig the run built, serialized.
//
// It is read after Run, and it is the half of this action that has nothing to do with the
// operator's own kubeconfig: phase.KubeConfig builds the credentials whether or not it writes
// them, and #305 separated the two so a caller could have the value without touching
// ~/.kube/config. Before this, the only way to reach it was to rebuild the phase list by hand.
//
// What comes back is cluster-admin credentials, so a caller holding them is holding the cluster.
func (a KubeConfig) Bytes() ([]byte, error) {
	if a.kubeconfig == nil {
		return nil, phase.ErrNoKubeConfig
	}
	return a.kubeconfig.Bytes()
}

// Config is the same credentials as a structure, for a caller that wants to read a field rather
// than a file. Nil until Run has built it.
func (a KubeConfig) Config() *clientcmdapi.Config {
	if a.kubeconfig == nil {
		return nil
	}
	return a.kubeconfig.Config()
}

// Run the actions
func (a KubeConfig) Run(ctx context.Context) error {
	l := logger.From(ctx)
	start := time.Now()
	a.Manager.SetPhases(a.Phases)

	if result := a.Manager.Run(ctx); result != nil {
		l.Info("apply failed", "error", result)
		return result
	}

	duration := time.Since(start).Truncate(time.Second)
	l.Info("finished in", "duration", duration)

	return nil
}
