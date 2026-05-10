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
	"fmt"
)

func (m *Cargoship) Build(
	ctx context.Context,
	// +ignore=[".gitignore"]
	// +defaultPath="bin"
	buildDir *dagger.Directory,
	// +ignore=[".gitignore"]
	// +defaultPath="."
	source *dagger.Directory,
) (*dagger.Directory, error) {
	if !m.IsInitialized {
		err := m.init(ctx, source)
		if err != nil {
			return nil, err
		}
	}

	goos := []string{"linux", "darwin", "windows"}
	goarch := []string{"amd64", "arm64"}

	temp := dag.Container().
		From("alpine:latest").
		WithExec([]string{"apk", "add", "--no-cache", "git"}).
		WithMountedDirectory("/src", source).
		WithWorkdir("/src")
	gitCommit, _ := temp.WithExec([]string{"git", "rev-parse", "--short", "HEAD", "--always"}).Stdout(ctx)

	for _, os := range goos {
		for _, arch := range goarch {
			// Defining binary file name
			binName := fmt.Sprintf("cargoship_%s_%s_%s", m.AppVersion, os, arch)
			if os == "windows" {
				binName += ".exe"
			}

			builder := dag.Container().
				From("golang:"+m.GoVersion+"-alpine").
				WithMountedCache("/go/pkg/mod", dag.CacheVolume("go-mod-"+m.GoVersion)).
				WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
				WithMountedCache("/go/build-cache", dag.CacheVolume("go-build-"+m.GoVersion)).
				WithEnvVariable("GOCACHE", "/go/build-cache").
				WithMountedDirectory("/src", source).
				WithWorkdir("/src").
				WithEnvVariable("GOOS", os).
				WithEnvVariable("GOARCH", arch)

			ldflagsArgs := LDFlags(ctx, m.AppVersion, gitCommit)

			builder = builder.WithExec([]string{
				"sh", "-c",
				fmt.Sprintf(`go build -v -ldflags "%s" -o /bin/%s /src/main.go`, ldflagsArgs, binName),
			})

			file := builder.File("/bin/" + binName)                         // Taking file from container
			buildDir = buildDir.WithFile(fmt.Sprintf("/%s", binName), file) // Adding file(bin) to dist directory
		}
	}

	return buildDir, nil
}
