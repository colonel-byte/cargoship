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

	"github.com/colonel-byte/zarf-distro/src/pkg/phase"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type ApplyOptions struct {
	// Manager is the phase manager
	Manager *phase.Manager
	// DisableDowngradeCheck skips the downgrade check
	DisableDowngradeCheck bool
	// NoWait skips waiting for the cluster to be ready
	NoWait bool
	// NoDrain skips draining worker nodes
	NoDrain bool
}

type Apply struct {
	ApplyOptions
	Phases phase.Phases
}

func NewApply(opts ApplyOptions) *Apply {
	apply := &Apply{
		ApplyOptions: opts,
		Phases: phase.Phases{
			&phase.Connect{},
			&phase.DetectOS{},
			&phase.PrepareHosts{},
			&phase.GatherFacts{},
			&phase.ValidateHosts{},

			&phase.Disconnect{},
		},
	}
	return apply
}

func (a Apply) Run(ctx context.Context) error {
	l := logger.From(ctx)
	start := time.Now()
	phase.NoWait = a.NoWait
	a.Manager.SetPhases(a.Phases)
	var result error

	if result = a.Manager.Run(ctx); result != nil {
		l.Info("apply failed", "error", result)
		return result
	}

	duration := time.Since(start).Truncate(time.Second)
	l.Info("finished in", "duration", duration)

	return nil
}
