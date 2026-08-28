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

// Package linux is implementing the interface github.com/colonel-byte/cargoship/src/types/os.Configurer for Linux based hosts
package linux

import (
	"strings"

	configurer "github.com/colonel-byte/cargoship/src/types/os"
	rigos "github.com/k0sproject/rig/v2/os"
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
	BaseLinux
}

var _ configurer.Configurer = (*Alpine)(nil)

func init() {
	configurer.RegisterOSModule(
		func(r *rigos.Release) bool {
			return r.ID == OSKindAlpine
		},
		func() any {
			return &Alpine{}
		},
	)
}

// InstallPackage installs packages via apk
func (l *Alpine) InstallPackage(h configurer.Host, pkg ...string) error {
	return h.Sudo().Exec("apk add --update " + strings.Join(pkg, " "))
}

// UninstallPackage installs packages via apk
func (l *Alpine) UninstallPackage(h configurer.Host, pkg ...string) error {
	return h.Sudo().Exec("apk del " + strings.Join(pkg, " "))
}

// Prepare will install required packages
func (l *Alpine) Prepare(h configurer.Host) error {
	return l.InstallPackage(h, "findutils", "coreutils")
}
