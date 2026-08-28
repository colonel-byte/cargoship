// Copyright 2023 harbor-cli authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from harbor-cli:
// https://github.com/goharbor/harbor-cli
//
// Modifications Copyright 2026 colonel-byte.
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

package main

import (
	"context"
	"dagger/cargoship/internal/dagger"

	"github.com/sourcegraph/conc/pool"
)

func (m *Cargoship) Build(
	ctx context.Context,
	// +ignore=[".gitignore"]
	// +defaultPath="."
	source *dagger.Directory,
	// Maximum number of platform builds to run concurrently. 0 runs all platforms at once
	// (the previous, unbounded behavior).
	// +optional
	// +default=0
	concurrency int,
) (*dagger.Directory, error) {
	if !m.IsInitialized {
		err := m.init(ctx, source)
		if err != nil {
			return nil, err
		}
	}

	buildDir := source.Directory("build")

	goos := []string{"linux", "darwin", "windows"}
	goarch := []string{"amd64", "arm64"}

	p := pool.NewWithResults[*dagger.File]().WithErrors().WithContext(ctx)
	if concurrency > 0 {
		p = p.WithMaxGoroutines(concurrency)
	}

	for _, os := range goos {
		for _, arch := range goarch {
			p.Go(func(ctx context.Context) (*dagger.File, error) {
				return m.BuildLocal(ctx, os, arch, source), nil
			})
		}
	}

	files, err := p.Wait()
	if err != nil {
		return nil, err
	}

	// Merging all binaries into buildDir in a single call, rather than chaining
	// buildDir = buildDir.WithFile(...) once per binary, keeps the resulting
	// Directory's dependency graph flat instead of a 6-deep linear chain. The
	// chained form forces the engine to resolve each build in series even
	// though the builds themselves are independent.
	return buildDir.WithFiles("/", files), nil
}
