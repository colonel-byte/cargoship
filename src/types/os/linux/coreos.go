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
	"strings"

	configurer "github.com/colonel-byte/cargoship/src/types/os"
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
)

const (
	// OSKindCoreOS id
	OSKindCoreOS = "CoreOS"
)

// CoreOS provides OS support for ostree based Fedora & RHEL systems
type CoreOS struct {
	os.Linux
	BaseLinux
}

var _ configurer.Configurer = (*CoreOS)(nil)

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return strings.Contains(os.Name, OSKindCoreOS) && (os.ID == OSKindELFedora || os.ID == OSKindELRedHat)
		},
		func() any {
			return &CoreOS{}
		},
	)
}

// InstallPackage installs packages but will throw an error
func (l *CoreOS) InstallPackage(_ os.Host, _ ...string) error {
	return errors.New("CoreOS does not support installing packages manually")
}

// UninstallPackage uninstalls packages but will throw an error
func (l *CoreOS) UninstallPackage(_ os.Host, _ ...string) error {
	return errors.New("CoreOS does not support removing packages manually")
}
