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

package action

import (
	"context"
	"time"

	"github.com/colonel-byte/cargoship/src/pkg/phase"
	"github.com/colonel-byte/cargoship/src/types/distrocfg"
	"github.com/colonel-byte/cargoship/src/types/distrocfg/registry"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// EngineConfigSyncOptions struct
type EngineConfigSyncOptions struct {
	// Manager is the phase manager
	Manager *phase.Manager
	// WorkerConcurrent number of workers that will be synced at a time, as a fixed
	// count ("5") or a percentage of the batch ("25%")
	WorkerConcurrent string
	// VaultPassword decrypts Ansible Vault-encrypted registry credentials
	VaultPassword string
	// LabelNodes whether to check and add the node-role.kubernetes.io/<profile> label on nodes
	LabelNodes bool
	// UpdateKubeConfig whether to update the local kubeconfig file with the admin creds for the cluster
	UpdateKubeConfig bool
}

// EngineConfigSync state logic
type EngineConfigSync struct {
	EngineConfigSyncOptions
	Phases phase.Phases
}

// NewEngineConfigSync an engine-config-sync action object
func NewEngineConfigSync(opts EngineConfigSyncOptions) *EngineConfigSync {
	disBuilder, err := registry.GetDistroModuleBuilder(opts.Manager.DistroID)
	if err != nil {
		return nil
	}

	if opts.Manager.Concurrency < 0 {
		opts.Manager.Concurrency = 0
	}

	d := disBuilder().(distrocfg.Distro) //nolint:errcheck

	lockPhase := &phase.Lock{}
	return &EngineConfigSync{
		EngineConfigSyncOptions: opts,
		Phases: phase.Phases{
			&phase.Connect{},

			&phase.DetectOS{},
			lockPhase,
			&phase.GatherFacts{},
			&phase.ValidateHosts{},
			&phase.GatherFactsDistro{
				Distro: d,
			},

			&phase.EngineConfigSyncController{
				EngineConfigSyncHosts: phase.EngineConfigSyncHosts{
					Distro:        d,
					VaultPassword: opts.VaultPassword,
				},
			},
			&phase.EngineConfigSyncWorker{
				EngineConfigSyncHosts: phase.EngineConfigSyncHosts{
					Distro:        d,
					VaultPassword: opts.VaultPassword,
				},
				WorkerConcurrent: opts.WorkerConcurrent,
			},
			&phase.KubeConfig{
				Distro:    d,
				ClusterID: opts.Manager.Config.Metadata.Name,
				Enabled:   opts.UpdateKubeConfig,
			},
			&phase.LabelNodes{
				Enabled: opts.UpdateKubeConfig && opts.LabelNodes,
			},

			lockPhase.UnlockPhase(),
			&phase.Disconnect{},
		},
	}
}

// Run the actions
func (r EngineConfigSync) Run(ctx context.Context) error {
	l := logger.From(ctx)
	start := time.Now()
	r.Manager.SetPhases(r.Phases)

	if result := r.Manager.Run(ctx); result != nil {
		l.Info("engine-config-sync failed", "error", result)
		return result
	}

	duration := time.Since(start).Truncate(time.Second)
	l.Info("finished in", "duration", duration)

	return nil
}
