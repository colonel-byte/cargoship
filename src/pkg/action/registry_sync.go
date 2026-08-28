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

// RegistrySyncOptions struct
type RegistrySyncOptions struct {
	// Manager is the phase manager
	Manager *phase.Manager
	// WorkerConcurrent number of workers that will be synced at a time
	WorkerConcurrent int
	// VaultPassword decrypts Ansible Vault-encrypted registry credentials
	VaultPassword string
}

// RegistrySync state logic
type RegistrySync struct {
	RegistrySyncOptions
	Phases phase.Phases
}

// NewRegistrySync a registry-sync action object
func NewRegistrySync(opts RegistrySyncOptions) *RegistrySync {
	disBuilder, err := registry.GetDistroModuleBuilder(opts.Manager.DistroID)
	if err != nil {
		return nil
	}

	if opts.WorkerConcurrent < 0 {
		opts.WorkerConcurrent = 0
	}

	if opts.Manager.Concurrency < 0 {
		opts.Manager.Concurrency = 0
	}

	d := disBuilder().(distrocfg.Distro) //nolint:errcheck

	lockPhase := &phase.Lock{}
	return &RegistrySync{
		RegistrySyncOptions: opts,
		Phases: phase.Phases{
			&phase.Connect{},

			&phase.DetectOS{},
			lockPhase,
			&phase.GatherFacts{},
			&phase.ValidateHosts{},
			&phase.GatherFactsDistro{
				Distro: d,
			},

			&phase.RegistrySyncController{
				RegistrySyncHosts: phase.RegistrySyncHosts{
					Distro:        d,
					VaultPassword: opts.VaultPassword,
				},
			},
			&phase.RegistrySyncWorker{
				RegistrySyncHosts: phase.RegistrySyncHosts{
					Distro:        d,
					VaultPassword: opts.VaultPassword,
				},
				WorkerConcurrent: opts.WorkerConcurrent,
			},

			lockPhase.UnlockPhase(),
			&phase.Disconnect{},
		},
	}
}

// Run the actions
func (r RegistrySync) Run(ctx context.Context) error {
	l := logger.From(ctx)
	start := time.Now()
	r.Manager.SetPhases(r.Phases)

	if result := r.Manager.Run(ctx); result != nil {
		l.Info("registry-sync failed", "error", result)
		return result
	}

	duration := time.Since(start).Truncate(time.Second)
	l.Info("finished in", "duration", duration)

	return nil
}
