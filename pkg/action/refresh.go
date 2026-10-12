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
	"fmt"
	"time"

	"github.com/colonel-byte/cargoship/pkg/phase"
	"github.com/colonel-byte/cargoship/types/distrocfg"
	"github.com/colonel-byte/cargoship/types/distrocfg/registry"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// RefreshOptions struct
type RefreshOptions struct {
	// Manager is the phase manager
	Manager *phase.Manager
}

// Refresh state logic
type Refresh struct {
	RefreshOptions
	Phases phase.Phases
}

// NewRefresh a refresh action object.
//
// Refresh is the read-only half of an apply: it connects to every host the configuration names,
// resolves each one's OS, gathers its network facts and reads the engine version already installed
// on it, and disconnects. Every phase in the list declares ReadOnly(), which is the property that
// makes the whole action safe to run against a live fleet -- nothing is written, and no cluster
// lock is taken, because Lock deliberately declares neither interface.
//
// It exists as an action rather than as a phase list each caller assembles because there is now
// more than one caller. The OpenTofu provider reads a fleet during a plan and during a refresh, and
// a provider that built its own list would be a second place where "which phases are safe to run"
// is decided -- which is exactly the question ReadOnly() exists to answer once. The CLI has no
// command for it yet; see docs/agent/design-tofu-provider-install.md.
//
// What it leaves behind is on the hosts themselves: each phase records what it learned on the
// ZarfHost it ran against, so a caller reads the facts off Manager.Config.Spec.Hosts afterwards
// rather than out of a return value.
func NewRefresh(opts RefreshOptions) (*Refresh, error) {
	if opts.Manager == nil {
		return nil, fmt.Errorf("a phase manager is required")
	}

	disBuilder, err := registry.GetDistroModuleBuilder(opts.Manager.DistroID)
	if err != nil {
		return nil, fmt.Errorf("no distro module for %q: %w", opts.Manager.DistroID, err)
	}
	d, ok := disBuilder().(distrocfg.Distro)
	if !ok {
		return nil, fmt.Errorf("the distro module for %q does not implement the distro interface", opts.Manager.DistroID)
	}

	return &Refresh{
		RefreshOptions: opts,
		Phases: phase.Phases{
			&phase.Connect{},
			&phase.DetectOS{},
			&phase.GatherFacts{},
			&phase.GatherFactsDistro{
				Distro: d,
				// A refresh reports what it found; it never routes an install, so the downgrade
				// refusal has nothing to refuse. Leaving it on would make reading a fleet fail
				// because of a package the caller has not named and may not have.
				AllowDowngrade: true,
			},
			&phase.Disconnect{},
		},
	}, nil
}

// Run the actions
func (a Refresh) Run(ctx context.Context) error {
	l := logger.From(ctx)
	start := time.Now()
	a.Manager.SetPhases(a.Phases)

	if result := a.Manager.Run(ctx); result != nil {
		l.Info("refresh failed", "error", result)
		return result
	}

	l.Debug("refreshed in", "duration", time.Since(start).Truncate(time.Second))
	return nil
}
