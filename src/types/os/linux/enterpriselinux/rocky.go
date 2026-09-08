// Copyright 2023 k0sctl authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from k0sctl:
// https://github.com/k0sproject/k0sctl
//
// Modifications Copyright 2026 colonel-byte.
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
	configurer "github.com/colonel-byte/cargoship/src/types/os"
	"github.com/colonel-byte/cargoship/src/types/os/linux"
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/os/registry"
)

// RockyLinux provides OS support for RockyLinux
type RockyLinux struct {
	linux.EnterpriseLinux
	configurer.Linux
}

var _ configurer.Configurer = (*RockyLinux)(nil)

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.ID == linux.OSKindELRocky
		},
		func() any {
			return &RockyLinux{}
		},
	)
}

func (r *RockyLinux) String() string {
	return "Rocky Linux"
}

// PreferredFirewall reports that Rocky Linux ships firewalld as its firewall front end.
func (r *RockyLinux) PreferredFirewall() string {
	return configurer.FirewallFirewalld
}
