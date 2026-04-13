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

package distro

import (
	"context"

	"github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/zarf-distro/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/zarf-distro/src/config"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type RancherCommon struct {
	Common
}

const test = `this is a test of things`

// ConfigureEngine implements Distro.
func (r *RancherCommon) ConfigureEngine(ctx context.Context, host cluster.ZarfHost, dis distro.ZarfDistro) error {
	newBaseConfig := dis.Spec.Config.Engine.Dup()

	logger.From(ctx).Warn("test", newBaseConfig.Dig(config.EngineConfig))

	// err := host.WriteFile(r.Config+".test", test, "0600")
	// if err != nil {
	// 	return err
	// }

	return nil
}
