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

var ErrDistroModuleNotFound = errors.New("distro support not found")

type (
	buildFunc = func() any
	matchFunc = func(string) bool
)

type distroFactory struct {
	MatchFunc matchFunc
	BuildFunc buildFunc
}

var distroModules []*distroFactory

func RegisterDistroModule(mf matchFunc, bf buildFunc) {
	// Inserting to beginning to match the most latest added
	distroModules = append([]*distroFactory{{MatchFunc: mf, BuildFunc: bf}}, distroModules...)
}

func GetDistroModuleBuilder(dis string) (buildFunc, error) {
	for _, of := range distroModules {
		if of.MatchFunc(dis) {
			return of.BuildFunc, nil
		}
	}

	return nil, ErrDistroModuleNotFound
}
