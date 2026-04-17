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
	"os"
	"os/signal"
	"syscall"

	"github.com/colonel-byte/zarf-distro/src/cmd"

	// anonymous import is needed to load the os configurers
	_ "github.com/colonel-byte/zarf-distro/src/types/os"
	// anonymous import is needed to load the os configurers
	_ "github.com/colonel-byte/zarf-distro/src/types/os/linux"
	// anonymous import is needed to load the os configurers
	_ "github.com/colonel-byte/zarf-distro/src/types/os/linux/enterpriselinux"
	// anonymous import is needed to load the distro configurers
	_ "github.com/colonel-byte/zarf-distro/src/types/distrocfg"
)

func main() {
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
