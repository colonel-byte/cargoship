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

// LatestTag pins the newest non-RC tag for a distro ("k3s" or "rke2") matching a
// major.minor prefix (e.g. "v1.35")
//
// Resolves the tag, prints it, and writes it into thirdparty-src/pins.json -- replacing
// whatever that minor line held, or adding the line if it is new. When the pin moves it
// re-pulls that version's source so pins.json and thirdparty-src/ never disagree. Touches
// the network.
//
//	mage generate:latestTag rke2 v1.35
func (Generate) LatestTag(distro, prefix string) error {
	pins, err := readEnginePins()
	if err != nil {
		return err
	}

	tag, err := pinTag(&pins, distro, prefix)
	if err != nil {
		return err
	}

	fmt.Println(tag)
	return nil
}
