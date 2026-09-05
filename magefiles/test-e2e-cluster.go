// Copyright 2026 colonel-byte
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build mage
// +build mage

package main

import (
	"fmt"
	"strings"

	"github.com/magefile/mage/sh"
)

// EndToEndCluster runs only the group that needs a bootloose cluster: the install command
// group. Needs Docker. It builds nothing: that suite calls the cargoship packages directly
// rather than driving a binary.
func (Test) EndToEndCluster() error {
	if err := stopBootlooseContainers(); err != nil {
		return err
	}
	return runE2ENoBuild("1h", "github.com/colonel-byte/cargoship/src/test/e2e/cluster/...")
}

// stopBootlooseContainers force-removes any leftover bootloose-managed containers (e.g. from
// a previous e2e run that was killed before cluster teardown ran), so requireCluster's bootloose
// Create() isn't confused by stale/exited containers with the same names.
func stopBootlooseContainers() error {
	ids, err := sh.Output("docker", "ps", "-aq", "--filter", "label=io.k0sproject.bootloose.owner=bootloose")
	if err != nil {
		return err
	}
	ids = strings.TrimSpace(ids)
	if ids == "" {
		return nil
	}
	fmt.Println("Removing leftover bootloose containers")
	return sh.RunV("docker", append([]string{"rm", "-f"}, strings.Fields(ids)...)...)
}
