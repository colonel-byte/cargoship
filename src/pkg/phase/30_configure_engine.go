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

	"github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/k0sproject/dig"
)

// ConfigureEngine writes the k0s configuration to host k0s config dir
type ConfigureEngine struct {
	GenericPhase
	leader        *cluster.ZarfHost
	configSource  int
	newBaseConfig dig.Mapping
	hosts         cluster.ZarfHosts
}

// Title returns the phase title
func (p *ConfigureEngine) Title() string {
	return "Configure engine"
}

func (p *ConfigureEngine) Run(ctx context.Context) error {
	return nil
}
