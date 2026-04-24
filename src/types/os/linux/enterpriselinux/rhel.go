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

package enterpriselinux

import (
	"strings"

	configurer "github.com/colonel-byte/cargoship/src/types/os"
	"github.com/colonel-byte/cargoship/src/types/os/linux"
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/os/registry"
)

// RHEL provides OS support for RedHat Enterprise Linux
type RHEL struct {
	linux.EnterpriseLinux
}

var _ configurer.Configurer = (*RHEL)(nil)

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.ID == linux.OSKindELRedHat && !strings.Contains(os.Name, linux.OSKindCoreOS)
		},
		func() any {
			return &RHEL{}
		},
	)
}

func (r *RHEL) String() string {
	return "Red Hat Enterprise Linux"
}
