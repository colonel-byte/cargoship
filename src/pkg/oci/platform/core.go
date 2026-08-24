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

// Package platform defines canonical CPU architecture identifiers and validation
// helpers used by cargoship when targeting specific platforms in OCI workflows.
package platform

import (
	"fmt"

	"github.com/colonel-byte/cargoship/src/config"
)

var (
	// ErrUnknownArch is returned when a Arch is not one of the supported architectures.
	ErrUnknownArch = fmt.Errorf("invalid platform operating system architecture")
)

// Arch names a CPU architecture an image volume can target.
type Arch string

// These are the supported architectures.
const (
	// ArchAMD64 is the amd64 architecture.
	ArchAMD64 Arch = config.OSArchAMD64
	// ArchARM64 is the arm64 architecture.
	ArchARM64 Arch = config.OSArchARM64
	// ArchRISCV is the riscv architecture.
	ArchRISCV Arch = config.OSArchRISCV
)

// ValidateArch checks if the given platform operating system architecture format is valid.
//
// format: the Arch to validate.
// error: an error if the architecture format is invalid, otherwise nil.
func ValidateArch(format Arch) error {
	switch format {
	case ArchAMD64, ArchARM64, ArchRISCV:
		return nil
	default:
		return ErrUnknownArch
	}
}
