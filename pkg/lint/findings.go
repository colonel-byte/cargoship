// Copyright 2021 zarf authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from zarf:
// https://github.com/zarf-dev/zarf
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

// Package lint contains functions for verifying yaml files are valid
package lint

import (
	"fmt"
)

// LintError represents an error containing lint findings.
type LintError struct {
	PackageName string
	Findings    []PackageFinding
}

func (e *LintError) Error() string {
	return fmt.Sprintf("linting error found %d instance(s)", len(e.Findings))
}

// OnlyWarnings returns true if all findings have severity warning.
func (e *LintError) OnlyWarnings() bool {
	for _, f := range e.Findings {
		if f.Severity == SevErr {
			return false
		}
	}
	return true
}

// Severity is the type of finding.
type Severity string

// Severity definitions.
const (
	SevErr  = "Error"
	SevWarn = "Warning"
)

// PackageFinding is a struct that contains a finding about something wrong with a package
type PackageFinding struct {
	// YqPath is the path to the key where the error originated from, this is sometimes empty in the case of a general error
	YqPath      string
	Description string
	// Item is the value of a key that is causing an error, for example a bad image name
	Item string
	// Severity of finding.
	Severity Severity
}

// ItemizedDescription returns a string with the description and item if finding contains one.
func (f PackageFinding) ItemizedDescription() string {
	if f.Item == "" {
		return f.Description
	}
	return fmt.Sprintf("%s - %s", f.Description, f.Item)
}
