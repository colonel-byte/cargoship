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

package build

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/magefile/mage/sh"
)

// ZarfModules are the Ansible module files of the colonel_byte.zarf collection, keyed by the
// binary name they are installed under. See docs/agent/choice-zarf-ansible-module.md.
var ZarfModules = map[string]string{ //nolint:gochecknoglobals
	"zarf_init":           "./cmd/zarf/init",
	"zarf_package_deploy": "./cmd/zarf/deploy",
}

// ZarfModuleFiles builds every zarf Ansible module file for this host into build/.
//
// They are built for the host rather than for a release target list because what drives them is
// the e2e suite and an operator reproducing a module run; the release builds come from
// .goreleaser.yaml. No version is stamped in: a module file reports what zarf did, and has no
// version of its own to report.
func ZarfModuleFiles() error {
	for name, pkg := range ZarfModules {
		out := filepath.Join(BuildDir, name)
		fmt.Println("building: " + out)

		if err := os.Remove(out); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		env := map[string]string{"CGO_ENABLED": "0"}
		if err := sh.RunWithV(env, "go", "build", "-trimpath", "-o", out, pkg); err != nil {
			return err
		}
	}
	return nil
}
