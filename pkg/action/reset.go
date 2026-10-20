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
	"time"

	"github.com/colonel-byte/cargoship/pkg/phase"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// ResetOptions struct
type ResetOptions struct {
	// Manager is the phase manager
	Manager *phase.Manager
	// NoDrain skips draining worker nodes
	NoDrain bool
	// DistroID is the type of Kubernetes engine that will be removed
	DistroID string
	// WorkerConcurrent number of workers that will be installed or upgraded at a time, as a fixed
	// count ("5") or a percentage of the batch ("25%")
	WorkerConcurrent string
	// TargetHosts names the hosts to delete and uninstall, by hostname or by the address the
	// configuration connects through. Empty means every host in the configuration, which is what
	// a reset has always done.
	//
	// The hosts not named still take part: leader selection scans the whole configuration, so a
	// controller that is staying is what the node deletions are driven through. That separation is
	// the whole point -- see #339.
	TargetHosts []string
}

// Reset state logic
type Reset struct {
	ResetOptions
	Phases phase.Phases
}

// NewReset an apply action object
func NewReset(opts ResetOptions) (*Reset, error) {
	d, err := distroModule(opts.Manager.DistroID)
	if err != nil {
		return nil, err
	}

	// A target that names no host would otherwise filter every phase down to nothing and report a
	// reset that removed nothing. Checked here rather than in the phases so it fails before a
	// single connection is opened.
	if opts.Manager != nil && opts.Manager.Config != nil {
		if err := phase.CheckTargetHosts(opts.Manager.Config.Spec.Hosts, opts.TargetHosts); err != nil {
			return nil, err
		}
	}

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

			&phase.DeleteWorkers{
				DeleteCommon: phase.DeleteCommon{
					Distro:      d,
					TargetHosts: opts.TargetHosts,
				},
				NoDrain:          opts.NoDrain,
				WorkerConcurrent: opts.WorkerConcurrent,
			},
			&phase.DeleteControllers{
				DeleteCommon: phase.DeleteCommon{
					Distro:      d,
					TargetHosts: opts.TargetHosts,
				},
				NoDrain: opts.NoDrain,
			},
			&phase.UninstallEngine{
				Distro:           d,
				TargetHosts:      opts.TargetHosts,
				WorkerConcurrent: opts.WorkerConcurrent,
			},

			// After the removal, not before: it reads the configuration the survivors
			// still carry, and what it writes is only correct once the run has decided
			// which controllers are going.
			&phase.ReconfigureSurvivors{
				Distro:      d,
				TargetHosts: opts.TargetHosts,
			},
			&phase.RemoveSelinuxPolicy{
				TargetHosts: opts.TargetHosts,
			},
			&phase.DaemonReload{
				TargetHosts: opts.TargetHosts,
			},

			lockPhase.UnlockPhase(),
			&phase.Disconnect{},
		},
	}

	return reset, nil
}

// Run the actions
func (r Reset) Run(ctx context.Context) error {
	l := logger.From(ctx)
	start := time.Now()
	r.Manager.SetPhases(r.Phases)

	if result := r.Manager.Run(ctx); result != nil {
		l.Info("reset failed", "error", result)
		return result
	}

	duration := time.Since(start).Truncate(time.Second)
	l.Info("finished in", "duration", duration)

	return nil
}
