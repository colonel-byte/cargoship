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
	"strings"

	configurer "github.com/colonel-byte/cargoship/src/types/os"
	rigos "github.com/k0sproject/rig/v2/os"
)

const (
	// OSKindCoreOS id
	OSKindCoreOS = "CoreOS"
)

// CoreOS provides OS support for ostree based Fedora & RHEL systems
type CoreOS struct {
	BaseLinux
}

var _ configurer.Configurer = (*CoreOS)(nil)

func init() {
	configurer.RegisterOSModule(
		func(r *rigos.Release) bool {
			return strings.Contains(r.Name, OSKindCoreOS) && (r.ID == OSKindELFedora || r.ID == OSKindELRedHat)
		},
		func() any {
			return &CoreOS{}
		},
	)
}

// InstallPackage installs packages but will throw an error
func (l *CoreOS) InstallPackage(_ configurer.Host, _ ...string) error {
	return errors.New("CoreOS does not support installing packages manually")
}

// UninstallPackage uninstalls packages but will throw an error
func (l *CoreOS) UninstallPackage(_ configurer.Host, _ ...string) error {
	return errors.New("CoreOS does not support removing packages manually")
}
