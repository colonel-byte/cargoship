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

package main

import (
	"fmt"
)

// UpdatePins refreshes every pinned minor line to its newest non-RC patch release
//
// Runs the same resolve-pin-pull cycle as Generate.LatestTag over every minor line already
// in thirdparty-src/pins.json, so a routine "are we behind upstream?" pass is one command
// rather than twelve. Touches the network.
func (Generate) UpdatePins() error {
	pins, err := readEnginePins()
	if err != nil {
		return err
	}

	// Snapshot the pinned minor lines first: pinTag rewrites Tags as it goes.
	type pinnedLine struct{ distro, prefix string }
	var lines []pinnedLine
	for _, d := range pins.Distros {
		for _, tag := range d.Tags {
			v, err := tagVersion(tag)
			if err != nil {
				return fmt.Errorf("%s: %s: %w", enginePinsPath, d.Name, err)
			}
			lines = append(lines, pinnedLine{d.Name, fmt.Sprintf("v%d.%d", v[0], v[1])})
		}
	}

	for _, l := range lines {
		if _, err := pinTag(&pins, l.distro, l.prefix); err != nil {
			return err
		}
	}

	fmt.Println("Run mage generate:engineConfig to regenerate structs for any version that moved.")
	return nil
}
