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

// ResetOptions struct
type ResetOptions struct {
	// Manager is the phase manager
	Manager *phase.Manager
	// NoWait skips waiting for the cluster to be ready
	NoWait bool
	// NoDrain skips draining worker nodes
	NoDrain bool
	// DistroID is the type of Kubernetes engine that will be removed
	DistroID string
	// WorkerConcurrent number of workers that will be installed or upgraded at a time
	WorkerConcurrent int
}

// Reset state logic
type Reset struct {
	ResetOptions
	Phases phase.Phases
}

// NewReset an apply action object
func NewReset(opts ResetOptions) *Reset {
	disBuilder, err := registry.GetDistroModuleBuilder(opts.Manager.DistroID)
	if err != nil {
		return nil
	}
	d := disBuilder().(distrocfg.Distro) //nolint:errcheck

	lockPhase := &phase.Lock{}
	reset := &Reset{
		ResetOptions: opts,
		Phases: phase.Phases{
			&phase.Connect{},

			&phase.DetectOS{},
			lockPhase,

			&phase.GatherFacts{},
			&phase.ValidateHosts{},
			&phase.GatherFactsDistro{
				Distro: d,
			},

			// &phase.DeleteWorkers{
			// 	Distro:           d,
			// 	NoDrain:          opts.NoDrain,
			// 	WorkerConcurrent: opts.WorkerConcurrent,
			// },
			// &phase.UninstallWorkers{
			// 	Distro:           d,
			// 	WorkerConcurrent: opts.WorkerConcurrent,
			// },

			&phase.DaemonReload{},
			lockPhase.UnlockPhase(),
			&phase.Disconnect{},
		},
	}

	return reset
}

// Run the actions
func (r Reset) Run(ctx context.Context) error {
	l := logger.From(ctx)
	start := time.Now()
	phase.NoWait = r.NoWait
	r.Manager.SetPhases(r.Phases)

	if result := r.Manager.Run(ctx); result != nil {
		l.Info("reset failed", "error", result)
		return result
	}

	duration := time.Since(start).Truncate(time.Second)
	l.Info("finished in", "duration", duration)

	return nil
}
