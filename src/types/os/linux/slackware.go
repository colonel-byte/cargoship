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
	// OSKindSlackware id
	OSKindSlackware = "slackware"
)

// Slackware provides OS support for Slackware Linux
type Slackware struct {
	BaseLinux
}

var _ configurer.Configurer = (*Slackware)(nil)

func init() {
	configurer.RegisterOSModule(
		func(r *rigos.Release) bool {
			return r.ID == OSKindSlackware
		},
		func() any {
			return &Slackware{}
		},
	)
}

// InstallPackage installs packages via slackpkg
func (l *Slackware) InstallPackage(h configurer.Host, pkg ...string) error {
	cmd := fmt.Sprintf("slackpkg update && slackpkg install --priority ADD %s", strings.Join(pkg, " "))
	return h.Sudo().Exec(cmd)
}

// UninstallPackage remove packages via slackpkg
func (l *Slackware) UninstallPackage(h configurer.Host, pkg ...string) error {
	return h.Sudo().Exec("slackpkg remove " + strings.Join(pkg, " "))
}
