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
	"github.com/colonel-byte/zarf-distro/src/pkg/phase"
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
		},
	}

	apply.Phases = append(apply.Phases, &phase.Disconnect{})
	return apply
}
