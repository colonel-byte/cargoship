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

// Package linux is implementing the interface github.com/colonel-byte/cargoship/src/types/os.Configurer for Linux based hosts
package linux

import (
	"strings"

	configurer "github.com/colonel-byte/cargoship/src/types/os"
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
)

const (
	// OSKindAlpine id
	OSKindAlpine = "alpine"
)

// BaseLinux for tricking go interfaces
type BaseLinux struct {
	configurer.Linux
}

// Alpine provides OS support for Alpine Linux
type Alpine struct {
	os.Linux
	BaseLinux
}

var _ configurer.Configurer = (*Alpine)(nil)

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.ID == OSKindAlpine
		},
		func() any {
			return &Alpine{}
		},
	)
}

// InstallPackage installs packages via apk
func (l *Alpine) InstallPackage(h os.Host, pkg ...string) error {
	return h.Execf("apk add --update %s", strings.Join(pkg, " "), exec.Sudo(h))
}

// UninstallPackage installs packages via apk
func (l *Alpine) UninstallPackage(h os.Host, pkg ...string) error {
	return h.Execf("apk del %s", strings.Join(pkg, " "), exec.Sudo(h))
}

// Prepare will install required packages
func (l *Alpine) Prepare(h os.Host) error {
	return l.InstallPackage(h, "findutils", "coreutils")
}
