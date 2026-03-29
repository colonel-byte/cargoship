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
	"path/filepath"

	"github.com/colonel-byte/zarf-distro/src/pkg/utils"
)

func (d *Distro) Create(ctx context.Context) error {
	if err := utils.ReadYAMLStrict(filepath.Join(d.cfg.CreateOpts.SourceDirectory, "distro.yaml"), &d.distro); err != nil {
		return err
	}

	if d.cfg.CreateOpts.Version != "" {
		d.distro.Metadata.Version = d.cfg.CreateOpts.Version
	}

	return nil
}
