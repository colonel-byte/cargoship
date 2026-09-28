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

//go:build mage
// +build mage

package main

import "github.com/colonel-byte/cargoship/magefiles/pkg/engine/source"

// UpdatePins refreshes every pinned minor line to its newest non-RC patch release
//
// Runs the same resolve-pin-pull cycle as Generate.LatestTag over every minor line already
// in thirdparty-src/pins.json, so a routine "are we behind upstream?" pass is one command
// rather than twelve. Touches the network.
func (Generate) UpdatePins() error {
	return source.UpdatePins()
}
