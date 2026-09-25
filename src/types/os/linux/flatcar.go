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
	"errors"

	configurer "github.com/colonel-byte/cargoship/src/types/os"
	rigos "github.com/k0sproject/rig/v2/os"
)

const (
	// OSKindFlatcar id
	OSKindFlatcar = "flatcar"
)

// Flatcar provides OS support for Flatcar systems
type Flatcar struct {
	BaseLinux
}

var _ configurer.Configurer = (*Flatcar)(nil)

func init() {
	configurer.RegisterOSModule(
		func(r *rigos.Release) bool {
			return r.ID == OSKindFlatcar
		},
		func() any {
			return &Flatcar{}
		},
	)
}

// InstallPackage installs packages but will throw an error
func (l *Flatcar) InstallPackage(_ configurer.Host, _ ...string) error {
	return errors.New("FlatcarContainerLinux does not support installing packages manually")
}

// UninstallPackage installs packages but will throw an error
func (l *Flatcar) UninstallPackage(_ configurer.Host, _ ...string) error {
	return errors.New("FlatcarContainerLinux does not support removing packages manually")
}
