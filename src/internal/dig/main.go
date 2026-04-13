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

package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/colonel-byte/zarf-distro/src/config"
	"github.com/colonel-byte/zarf-distro/src/pkg/packager/load"
	"github.com/k0sproject/dig"
)

const (
	test = "example/rke2/distro.yaml"
)

func main() {
	ctx := context.TODO()
	m := dig.Mapping{}
	m["tls"] = []string{
		"test-kc01.example.com",
		"test-kc01",
	}
	path, err := filepath.Abs(test)
	if err != nil {
		panic(err)
	}
	distro, err := load.DistroDefinition(ctx, path, load.DefinitionOptions{
		CachePath: "~/.zarf-cache",
	})
	if err != nil {
		panic(err)
	}
	cfg := distro.Spec.Config.Engine.DigMapping(config.EngineConfig)
	fmt.Println(cfg)
	cfg.Merge(m, dig.WithOverwrite())
	fmt.Println(cfg)
}
