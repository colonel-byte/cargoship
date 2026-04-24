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

package registry

import (
	"errors"
)

// ErrDistroModuleNotFound is used when a distro module is not found in the registry
var ErrDistroModuleNotFound = errors.New("distro support not found")

type (
	// BuildFunc is a function returns a basic distro object
	BuildFunc = func() any
	// MatchFunc is a function that takes in an id string and returns if it matches a the current distro
	MatchFunc = func(id string) bool
)

type distroFactory struct {
	MatchFunc MatchFunc
	BuildFunc BuildFunc
}

var distroModules []*distroFactory

// RegisterDistroModule takes a MatchFunc and BuildFunc and adds them to the registry of known distros.
func RegisterDistroModule(mf MatchFunc, bf BuildFunc) {
	// Inserting to beginning to match the most latest added
	distroModules = append([]*distroFactory{{MatchFunc: mf, BuildFunc: bf}}, distroModules...)
}

// GetDistroModuleBuilder returns a BuildFunc used to construct a distro object
func GetDistroModuleBuilder(dis string) (BuildFunc, error) {
	for _, of := range distroModules {
		if of.MatchFunc(dis) {
			return of.BuildFunc, nil
		}
	}

	return nil, ErrDistroModuleNotFound
}
