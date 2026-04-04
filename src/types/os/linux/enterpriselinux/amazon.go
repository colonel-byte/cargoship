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
	configurer "github.com/colonel-byte/zarf-distro/src/types/os"
	"github.com/colonel-byte/zarf-distro/src/types/os/linux"
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
)

// AmazonLinux provides OS support for AmazonLinux
type AmazonLinux struct {
	linux.EnterpriseLinux
	configurer.Linux
}

var _ configurer.Configurer = (*AmazonLinux)(nil)

// Hostname on amazon linux will return the full hostname
func (l *AmazonLinux) Hostname(h os.Host) string {
	hostname, _ := h.ExecOutput("hostname")

	return hostname
}

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.ID == linux.OS_KIND_EL_AMAZON
		},
		func() any {
			return &AmazonLinux{}
		},
	)
}
