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

package linux

import (
	"fmt"
	"strings"

	configurer "github.com/colonel-byte/cargoship/src/types/os"
)

const (
	// OSKindELAlma id
	OSKindELAlma = "almalinux"
	// OSKindELAmazon id
	OSKindELAmazon = "amzn"
	// OSKindELCent id
	OSKindELCent = "centos"
	// OSKindELFedora id
	OSKindELFedora = "fedora"
	// OSKindELOracle id
	OSKindELOracle = "ol"
	// OSKindELRedHat id
	OSKindELRedHat = "rhel"
	// OSKindELRocky id
	OSKindELRocky = "rocky"
)

// EnterpriseLinux is a base package for several RHEL-like enterprise linux distributions
type EnterpriseLinux struct {
	configurer.Linux
}

// InstallPackage installs packages via dnf
func (c *EnterpriseLinux) InstallPackage(h configurer.Host, s ...string) error {
	if err := h.Sudo().Exec("dnf install -y --nogpgcheck " + strings.Join(s, " ")); err != nil {
		return fmt.Errorf("failed to install packages: %w", err)
	}
	return nil
}

// UninstallPackage uninstalls packages via dnf
func (c *EnterpriseLinux) UninstallPackage(h configurer.Host, s ...string) error {
	if err := h.Sudo().Exec("dnf remove -y " + strings.Join(s, " ")); err != nil {
		return fmt.Errorf("failed to uninstall packages: %w", err)
	}
	return nil
}
