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
	configurer "github.com/colonel-byte/zarf-distro/src/types/os"
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/os/registry"
)

const (
	OS_KIND_OPENSUSE       = "opensuse"
	OS_KIND_OPENSUSE_MICRO = "opensuse-microos"
)

// OpenSUSE provides OS support for OpenSUSE
type OpenSUSE struct {
	SLES
}

var _ configurer.Configurer = (*OpenSUSE)(nil)

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.ID == OS_KIND_OPENSUSE || os.ID == OS_KIND_OPENSUSE_MICRO
		},
		func() any {
			return &OpenSUSE{}
		},
	)
}
