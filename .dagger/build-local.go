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
	"dagger/cargoship/utils"
	"fmt"
)

// Create build of Cargoship for local testing and development
func (m *Cargoship) BuildLocal(
	ctx context.Context,
	os string,
	arch string,
	// +ignore=[".gitignore"]
	// +defaultPath="."
	source *dagger.Directory,
) *dagger.File {
	if !m.IsInitialized {
		err := m.init(ctx, source)
		if err != nil {
			return nil
		}
	}

	binName := fmt.Sprintf("cargoship_%s_%s", os, arch)

	builder := dag.Container().
		From("golang:"+m.GoVersion).
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("go-mod-"+m.GoVersion)).
		WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
		WithMountedCache("/go/build-cache", dag.CacheVolume("go-build-"+m.GoVersion)).
		WithEnvVariable("GOCACHE", "/go/build-cache").
		WithMountedDirectory("/src", m.Source). // Ensure the source directory with go.mod is mounted
		WithWorkdir("/src").
		WithEnvVariable("GOOS", os).
		WithEnvVariable("GOARCH", arch)

	gitCommit, _ := builder.WithExec([]string{"git", "rev-parse", "--short", "HEAD", "--always"}).Stdout(ctx)

	ldflagsArgs := utils.LDFlags(m.AppVersion, gitCommit)
	gcflagsArgs := utils.GCFLags()

	builder = builder.WithExec([]string{
		"sh",
		"-c",
		fmt.Sprintf(`go build -a -gcflags=all="%s" -ldflags "%s" -o /bin/%s /src/main.go`, gcflagsArgs, ldflagsArgs, binName),
	})
	return builder.File("/bin/" + binName)
}
