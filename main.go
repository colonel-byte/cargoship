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
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/colonel-byte/zarf-distro/src/cmd"
	"github.com/colonel-byte/zarf-distro/src/pkg/utils"
	"github.com/colonel-byte/zarf-distro/src/types"
	goyaml "github.com/goccy/go-yaml"
)

func main() {
	Cobra()
}

func Print() {
	cn := types.DistroConfig{}
	if err := utils.ReadYAMLStrict(filepath.Join(".", "zarf-distro-config1.yaml"), &cn); err != nil {
		fmt.Println("fml")
	}

	bytes, err := goyaml.Marshal(cn)
	if err != nil {
		fmt.Println("fml - v2")
	}
	fmt.Println(string(bytes))

	fmt.Println(cn.DistroOpts.OCIConcurrency)
}

func Cobra() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		first := true
		for {
			<-signalCh
			if first {
				first = false
				cancel()
				continue
			}
			os.Exit(1)
		}
	}()

	if err := cmd.Execute(ctx); err != nil {
		os.Exit(1)
	}
}
