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
	rigos "github.com/k0sproject/rig/v2/os"
)

const (
	// OSKindDebian id
	OSKindDebian = "debian"
)

// Debian provides OS support for Debian systems
type Debian struct {
	configurer.Linux
}

var _ configurer.Configurer = (*Debian)(nil)

func init() {
	configurer.RegisterOSModule(
		func(r *rigos.Release) bool {
			return r.ID == OSKindDebian
		},
		func() any {
			return &Debian{}
		},
	)
}

// InstallPackage installs packages via apt-get
func (c Debian) InstallPackage(h configurer.Host, s ...string) error {
	sudo := h.Sudo()
	if err := sudo.Exec("apt-get update"); err != nil {
		return fmt.Errorf("failed to update apt cache: %w", err)
	}
	if err := sudo.Exec("DEBIAN_FRONTEND=noninteractive apt-get install -y -q " + strings.Join(s, " ")); err != nil {
		return fmt.Errorf("failed to install packages: %w", err)
	}
	return nil
}

// UninstallPackage uninstalls packages via apt-get
func (c Debian) UninstallPackage(h configurer.Host, s ...string) error {
	if err := h.Sudo().Exec("DEBIAN_FRONTEND=noninteractive apt-get remove -y -q " + strings.Join(s, " ")); err != nil {
		return fmt.Errorf("failed to remove packages: %w", err)
	}
	return nil
}
