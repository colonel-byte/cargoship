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

package os

import (
	"sync"

	rigos "github.com/k0sproject/rig/v2/os"
)

// osModuleEntry pairs a match predicate with a builder function for a
// registered OS support module.
type osModuleEntry struct {
	match   func(*rigos.Release) bool
	builder func() any
}

var (
	registryMu sync.RWMutex
	osModules  []osModuleEntry
)

// RegisterOSModule registers an OS support module. Modules are matched in
// most-recently-registered-first order, mirroring the behavior of the
// (now removed in rig v2) github.com/k0sproject/rig/os/registry package
// that this replaces.
func RegisterOSModule(match func(*rigos.Release) bool, builder func() any) {
	registryMu.Lock()
	defer registryMu.Unlock()
	osModules = append([]osModuleEntry{{match: match, builder: builder}}, osModules...)
}

// ResolveOSModule returns the builder function for the first registered
// module whose matcher accepts release, or false if none match.
func ResolveOSModule(release *rigos.Release) (func() any, bool) {
	if release == nil {
		return nil, false
	}
	registryMu.RLock()
	defer registryMu.RUnlock()
	for _, entry := range osModules {
		if entry.match(release) {
			return entry.builder, true
		}
	}
	return nil, false
}
