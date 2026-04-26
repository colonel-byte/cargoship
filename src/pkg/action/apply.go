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

// Package action are various actions used by the package
package action

import (
	"context"
	"time"

	"github.com/colonel-byte/cargoship/src/pkg/phase"
	"github.com/colonel-byte/cargoship/src/types/distrocfg"
	"github.com/colonel-byte/cargoship/src/types/distrocfg/registry"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// ApplyOptions struct
type ApplyOptions struct {
	// Manager is the phase manager
	Manager *phase.Manager
	// DisableDowngradeCheck skips the downgrade check
	DisableDowngradeCheck bool
	// NoWait skips waiting for the cluster to be ready
	NoWait bool
	// NoDrain skips draining worker nodes
	NoDrain bool
	// ModifyHosts updates the /etc/hosts file with all the nodes in the cluster
	ModifyHosts bool
	// ModifyFirewall updates the firewalld on the nodes
	ModifyFirewall bool
	// WorkerConcurrent number of workers that will be installed or upgraded at a time
	WorkerConcurrent int
}

// Apply state logic
type Apply struct {
	ApplyOptions
	Phases phase.Phases
}

// NewApply an apply action object
func NewApply(opts ApplyOptions) *Apply {
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
	apply := &Apply{
		ApplyOptions: opts,
		Phases: phase.Phases{
			&phase.Connect{},

			&phase.DetectOS{},
			lockPhase,
			&phase.GatherFacts{},
			&phase.ValidateHosts{},
			&phase.PrepareHosts{},
			&phase.PrepareSelinux{},
			&phase.PrepareFapolicy{},
			&phase.GatherFactsDistro{
				Distro: d,
			},
			&phase.ModifyHosts{
				Enabled: opts.ModifyHosts,
			},
			&phase.ConfigureFirewall{
				Distro:  d,
				Enabled: opts.ModifyFirewall,
			},
			&phase.ConfigureFirewallPorts{
				Enabled: opts.ModifyFirewall,
			},

			&phase.UploadFiles{},
			&phase.RPMUploadFiles{},
			&phase.APTUploadFiles{},
			&phase.BINUploadFiles{
				Distro: d,
			},

			&phase.ConfigureEngine{
				Distro: d,
			},
			&phase.InitializeControllers{
				Distro: d,
			},
			&phase.InitializeWorkers{
				Distro:           d,
				WorkerConcurrent: opts.WorkerConcurrent,
			},
			&phase.UpgradeController{
				UpgradeHosts: phase.UpgradeHosts{
					Distro: d,
				},
			},
			&phase.UpgradeWorkers{
				UpgradeHosts: phase.UpgradeHosts{
					Distro: d,
				},
				WorkerConcurrent: opts.WorkerConcurrent,
			},

			lockPhase.UnlockPhase(),
			&phase.Disconnect{},
		},
	}

	return apply
}

// Run the actions
func (a Apply) Run(ctx context.Context) error {
	l := logger.From(ctx)
	start := time.Now()
	phase.NoWait = a.NoWait
	a.Manager.SetPhases(a.Phases)

	if result := a.Manager.Run(ctx); result != nil {
		l.Info("apply failed", "error", result)
		return result
	}

	duration := time.Since(start).Truncate(time.Second)
	l.Info("finished in", "duration", duration)

	return nil
}
