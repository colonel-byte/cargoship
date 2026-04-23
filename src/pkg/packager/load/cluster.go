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

	v1alpha1 "github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/internal/clustercfg"
	"github.com/colonel-byte/cargoship/src/pkg/packager/layout"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type ClusterOptions struct{}

func ClusterDefinition(ctx context.Context, configPath string, opts ClusterOptions) (v1alpha1.ZarfCluster, error) {
	l := logger.From(ctx)
	start := time.Now()
	l.Debug("start layout.ClusterDefinition", "path", configPath)

	conPath, err := layout.ResolveClusterPath(configPath)
	if err != nil {
		return v1alpha1.ZarfCluster{}, err
	}

	b, err := os.ReadFile(conPath.ManifestFile)
	if err != nil {
		return v1alpha1.ZarfCluster{}, err
	}
	cluster, err := clustercfg.Parse(ctx, b)
	if err != nil {
		return v1alpha1.ZarfCluster{}, err
	}

	err = validateCluster(ctx, cluster, conPath.ManifestFile)
	if err != nil {
		return v1alpha1.ZarfCluster{}, err
	}
	l.Debug("done layout.ClusterDefinition", "duration", time.Since(start))
	return cluster, nil
}

func validateCluster(_ context.Context, _ v1alpha1.ZarfCluster, _ string) error {
	return nil
}
