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

// KubeConfigOptions struct
type KubeConfigOptions struct {
	// Manager is the phase manager
	Manager *phase.Manager
}

// KubeConfig state logic
type KubeConfig struct {
	KubeConfigOptions
	Phases phase.Phases
}

// NewKubeConfig pulls the admin cert from a control-plane node and updates the local kube-config
func NewKubeConfig(opts KubeConfigOptions) *KubeConfig {
	disBuilder, err := registry.GetDistroModuleBuilder(opts.Manager.DistroID)
	if err != nil {
		return nil
	}

	d := disBuilder().(distrocfg.Distro) //nolint:errcheck

	lockPhase := &phase.Lock{}
	config := &KubeConfig{
		KubeConfigOptions: opts,
		Phases: phase.Phases{
			&phase.Connect{},
			&phase.KubeConfig{
				Distro:    d,
				ClusterID: opts.Manager.Config.Metadata.Name,
				Enabled:   true,
			},
			lockPhase.UnlockPhase(),
			&phase.Disconnect{},
		},
	}

	return config
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
