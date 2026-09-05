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

// Package api defines types shared across cargoship's API groups.
package api

import (
	"fmt"
	"slices"
	"strings"

	"github.com/invopop/jsonschema"
)

// ErrUnknownArch is returned for a CPU architecture cargoship does not support.
var ErrUnknownArch = fmt.Errorf("invalid platform operating system architecture")

// Arch names a CPU architecture a package can target.
type Arch string

// These are the CPU architectures cargoship supports targeting.
const (
	// ArchAMD64 is the x86-64, 64-bit AMD, architecture.
	ArchAMD64 Arch = "amd64"
	// ArchARM64 is the 64-bit ARM architecture.
	ArchARM64 Arch = "arm64"
	// ArchRISCV is the 64-bit RISC-V architecture.
	ArchRISCV Arch = "riscv64"
)

// arches is the single source of truth for which architectures cargoship supports. Validate and the
// generated schema both read from it.
var (
	arches = Arches{
		ArchAMD64,
		ArchARM64,
		ArchRISCV,
	}
)

// Validate reports whether the architecture is one cargoship supports.
func (a Arch) Validate() error {
	if !slices.Contains(arches, a) {
		return fmt.Errorf("%q: %w", string(a), ErrUnknownArch)
	}

	return nil
}

// JSONSchemaExtend lists the supported architectures as the enum for a single architecture, so the
// schema and Validate stay in step with the arches list.
func (Arch) JSONSchemaExtend(s *jsonschema.Schema) {
	for _, arch := range arches {
		s.Enum = append(s.Enum, string(arch))
	}
}

// Arches is a list of CPU architectures.
type Arches []Arch

// JSONSchemaExtend applies the supported architecture enum to each item in the list and requires the
// entries to be unique.
func (Arches) JSONSchemaExtend(s *jsonschema.Schema) {
	s.UniqueItems = true
	if s.Items == nil {
		s.Items = &jsonschema.Schema{Type: "string"}
	}
	if len(s.Items.Enum) > 0 {
		return
	}
	for _, arch := range arches {
		s.Items.Enum = append(s.Items.Enum, string(arch))
	}
}

// ParseArch converts a raw architecture string into an Arch, rejecting anything cargoship does not
// support. Use it at the boundaries where an architecture arrives as free text, such as a command
// line flag or the output of uname on a remote host.
func ParseArch(s string) (Arch, error) {
	a := Arch(s)
	if err := a.Validate(); err != nil {
		return "", err
	}

	return a, nil
}

// FormatArches renders architectures as a comma separated list, for error messages and logs.
func FormatArches(arches Arches) string {
	parts := make([]string, 0, len(arches))
	for _, a := range arches {
		parts = append(parts, string(a))
	}

	return strings.Join(parts, ", ")
}
