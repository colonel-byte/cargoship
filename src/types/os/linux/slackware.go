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
	"fmt"
	"strings"

	configurer "github.com/colonel-byte/cargoship/src/types/os"
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
)

const (
	OS_KIND_SLACKWARE = "slackware"
)

// Slackware provides OS support for Slackware Linux
type Slackware struct {
	BaseLinux
	os.Linux
}

var _ configurer.Configurer = (*Slackware)(nil)

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.ID == OS_KIND_SLACKWARE
		},
		func() any {
			return &Slackware{}
		},
	)
}

// InstallPackage installs packages via slackpkg
func (l *Slackware) InstallPackage(h os.Host, pkg ...string) error {
	updatecmd, err := h.Sudo("slackpkg update")
	if err != nil {
		return err
	}
	installcmd, err := h.Sudo(fmt.Sprintf("slackpkg install --priority ADD %s", strings.Join(pkg, " ")))
	if err != nil {
		return err
	}

	return h.Execf("%s && %s", updatecmd, installcmd)
}
