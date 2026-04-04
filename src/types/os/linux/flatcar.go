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
	"errors"

	"github.com/colonel-byte/zarf-distro/src/types/distro"
	configurer "github.com/colonel-byte/zarf-distro/src/types/os"
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
)

const (
	OS_KIND_FLATCAR = "flatcar"
)

type Flatcar struct {
	BaseLinux
	os.Linux
}

var _ configurer.Configurer = (*Flatcar)(nil)

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.ID == OS_KIND_FLATCAR
		},
		func() any {
			return &Flatcar{}
		},
	)
}

func (l *Flatcar) InstallPackage(h os.Host, pkg ...string) error {
	return errors.New("FlatcarContainerLinux does not support installing packages manually")
}

// HostPath returns the given path unchanged for linux hosts
func (l *Flatcar) ConfigureDistro(dis distro.Distro) {
	dis.SetPath(distro.BinaryDir, "/opt/bin")
}
