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

	configurer "github.com/colonel-byte/mare/src/types/os"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/linux"
)

const (
	OS_KIND_EL_ALMA    = "almalinux"
	OS_KIND_EL_AMAZON  = "amzn"
	OS_KIND_EL_CENTOS  = "centos"
	OS_KIND_EL_FEDORA  = "fedora"
	OS_KIND_EL_ORACLE  = "ol"
	OS_KIND_EL_RED_HAT = "rhel"
	OS_KIND_EL_ROCKY   = "rocky"
)

// EnterpriseLinux is a base package for several RHEL-like enterprise linux distributions
type EnterpriseLinux struct {
	linux.EnterpriseLinux
	configurer.Linux
}

// InstallPackage installs packages via dnf
func (c *EnterpriseLinux) InstallPackage(h os.Host, s ...string) error {
	if err := h.Execf("dnf install -y %s", strings.Join(s, " "), exec.Sudo(h)); err != nil {
		return fmt.Errorf("failed to install packages: %w", err)
	}

	return nil
}
