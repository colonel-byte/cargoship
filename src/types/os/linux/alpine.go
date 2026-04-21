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

package linux

import (
	"strings"

	configurer "github.com/colonel-byte/mare/src/types/os"
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
)

const (
	OS_KIND_ALPINE = "alpine"
)

// BaseLinux for tricking go interfaces
type BaseLinux struct {
	configurer.Linux
}

// Alpine provides OS support for Alpine Linux
type Alpine struct {
	os.Linux
	BaseLinux
}

var _ configurer.Configurer = (*Alpine)(nil)

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.ID == OS_KIND_ALPINE
		},
		func() any {
			return &Alpine{}
		},
	)
}

// InstallPackage installs packages via slackpkg
func (l *Alpine) InstallPackage(h os.Host, pkg ...string) error {
	return h.Execf("apk add --update %s", strings.Join(pkg, " "), exec.Sudo(h))
}

func (l *Alpine) Prepare(h os.Host) error {
	return l.InstallPackage(h, "findutils", "coreutils")
}
