// Copyright 2026 colonel-byte
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package build handles compiling cargoship binaries with appropriate compiler and linker flags.
package build

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/magefiles/pkg/util"
	buildutil "github.com/colonel-byte/cargoship/pkg/utils/build"
	"github.com/magefile/mage/sh"
)

// Binary compiles a cargoship binary for the specified OS and architecture into build/.
func Binary(oper, arch string) error {
	bin := fmt.Sprintf("build/cargoship_%s_%s", oper, arch)
	fmt.Println("building: " + bin)

	if err := os.Remove(bin); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	env := map[string]string{
		"GOOS":        oper,
		"GOARCH":      arch,
		"CGO_ENABLED": "0",
	}

	gc := buildutil.GCFLags()
	ld := buildutil.LDFlags(config.UnsetCLIVersion, util.GitCommit())

	goBuild := fmt.Sprintf(`go build -trimpath -gcflags=all="%s" -ldflags "%s" -o %s ./main.go`, gc, ld, bin)
	fmt.Println("executing:\n  " + goBuild)

	return sh.RunWithV(env, "sh", "-c", goBuild)
}

// All compiles release binaries for all standard release OS/arch targets.
func All() error {
	targets := [][2]string{
		{"linux", "amd64"},
		{"linux", "arm64"},
		{"darwin", "amd64"},
		{"darwin", "arm64"},
	}
	for _, t := range targets {
		if err := Binary(t[0], t[1]); err != nil {
			fmt.Printf("got an error: %v\n", err)
		}
	}
	return nil
}
