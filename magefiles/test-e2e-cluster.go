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
	"os"
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

// EndToEndClusterStage runs the same suite as EndToEndCluster, but stops at the boundary
// phase/60 draws: it stages the files and renders the engine config without starting the
// engine on any node, and it provisions five machines rather than ten. It takes a few minutes
// rather than tens of them and brings up no rke2 cluster, which is what makes it runnable
// somewhere a nine-node cluster is not. Use EndToEndCluster for the walk that bootstraps.
func (Test) EndToEndClusterStage() error {
	if err := stopBootlooseContainers(); err != nil {
		return err
	}
	if err := os.Setenv("CARGOSHIP_E2E_STAGE_ONLY", "1"); err != nil {
		return err
	}
	return runE2ENoBuild("30m", "github.com/colonel-byte/cargoship/src/test/e2e/cluster/...")
}

// CleanCluster removes the containers a bootloose cluster left behind. EndToEndCluster does
// this before it runs, so this target is for the run that was killed partway through and left
// nine nodes holding memory, or for looking at what a failed run left and then clearing it.
func (Test) CleanCluster() error {
	return stopBootlooseContainers()
}

// EndToEndClusterUpgrade runs the same group with the upgrade walk turned on, which installs
// the example distro and then upgrades the cluster to the next patch release one phase at a
// time. It roughly doubles the disk and the runtime of EndToEndCluster, which is why it is a
// separate target rather than the default.
func (Test) EndToEndClusterUpgrade() error {
	if err := stopBootlooseContainers(); err != nil {
		return err
	}
	if err := os.Setenv("CARGOSHIP_E2E_UPGRADE", "1"); err != nil {
		return err
	}
	return runE2ENoBuild("3h", "github.com/colonel-byte/cargoship/src/test/e2e/cluster/...")
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
	// -v takes the anonymous volumes with the containers. Without it a local run leaves one
	// behind per machine per run, because Docker does not reap them with the container that
	// declared them -- see engineData in src/test/e2e/cluster/main_test.go.
	return sh.RunV("docker", append([]string{"rm", "-fv"}, strings.Fields(ids)...)...)
}
