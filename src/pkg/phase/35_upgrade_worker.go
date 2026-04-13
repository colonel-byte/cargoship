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

package phase

import (
	"context"

	"github.com/colonel-byte/zarf-distro/src/types/distro"
)

type UpgradeWorkers struct {
	GenericPhase
	Distro distro.Distro
}

func (p *UpgradeWorkers) Title() string {
	return "Upgrade Worker"
}

func (p *UpgradeWorkers) Run(ctx context.Context) error {
	return nil
}
