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

	"github.com/colonel-byte/zarf-distro/src/pkg/distro"
	"github.com/colonel-byte/zarf-distro/src/types"
)

func main() {
	rootCtx := context.TODO()
	opt := types.DistroConfig{
		CreateOpts: types.DistroCreateOptions{
			SourceDirectory: "schema",
			Version:         "v1.1.1",
			CachePath:       "build",
		},
	}

	dis, err := distro.New(&opt)

	if err != nil {
		fmt.Printf("got the following: %v", err)
		os.Exit(1)
	}

	dis.Create(rootCtx)
}
