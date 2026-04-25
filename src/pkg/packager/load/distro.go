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

package load

import (
	"context"
	"os"
	"time"

	v1alpha1 "github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/internal/distrocfg"
	"github.com/colonel-byte/cargoship/src/pkg/packager/layout"
	"github.com/zarf-dev/zarf/src/config"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/types"
)

// DefinitionOptions are the optional parameters to load.PackageDefinition
type DefinitionOptions struct {
	CachePath string
	types.RemoteOptions
}

// DistroDefinition returns a ZarfDistro object
func DistroDefinition(ctx context.Context, distroPath string, _ DefinitionOptions) (v1alpha1.ZarfDistro, error) {
	l := logger.From(ctx)
	start := time.Now()
	l.Debug("start layout.DistroDefinition", "path", distroPath)

	disPath, err := layout.ResolveDistroPath(distroPath)
	if err != nil {
		return v1alpha1.ZarfDistro{}, err
	}

	b, err := os.ReadFile(disPath.ManifestFile)
	if err != nil {
		return v1alpha1.ZarfDistro{}, err
	}
	dis, err := distrocfg.Parse(ctx, b)
	if err != nil {
		return v1alpha1.ZarfDistro{}, err
	}
	dis.Metadata.Architecture = config.GetArch(dis.Metadata.Architecture)

	err = validateDistro(ctx, dis, disPath.ManifestFile)
	if err != nil {
		return v1alpha1.ZarfDistro{}, err
	}
	l.Debug("done layout.DistroDefinition", "duration", time.Since(start))
	return dis, nil
}

func validateDistro(_ context.Context, _ v1alpha1.ZarfDistro, _ string) error {
	return nil
}
