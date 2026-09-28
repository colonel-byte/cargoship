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
	"os"

	"github.com/magefile/mage/mg"
)

type (
	Test mg.Namespace
)

// EndToEnd runs the whole e2e suite: both the cluster and non-cluster groups, including the
// example packages that pull ~1.5GB of engine artifacts and images. Needs Docker.
func (Test) EndToEnd() error {
	if err := stopBootlooseContainers(); err != nil {
		return err
	}
	return runE2E("1h", "github.com/colonel-byte/cargoship/test/e2e/...")
}

// EndToEndNonCluster runs the group that needs no cluster: the misc and package command
// groups. -short additionally skips the example packages, so this finishes in seconds.
// Mirrors the e2e-noncluster CI job.
func (Test) EndToEndNonCluster() error {
	return runE2E("30m", "github.com/colonel-byte/cargoship/test/e2e/noncluster/...", "-short")
}

// EndToEndCluster runs the full install-command-group suite, including the phases that install
// and bootstrap k3s. Needs Docker. It builds nothing: that suite calls the cargoship packages
// directly rather than driving a binary.
//
// This is a local-box target. CI only runs EndToEndClusterStage -- a free hosted runner is not
// reliable enough to bootstrap a real cluster on, see docs/agent/choice-e2e-stage-split.md --
// so this is the only way to exercise the engine-bootstrap phases, phase/61 onward, end to end.
func (Test) EndToEndCluster() error {
	if err := stopBootlooseContainers(); err != nil {
		return err
	}
	return runE2ENoBuild("1h", "github.com/colonel-byte/cargoship/test/e2e/cluster/...")
}

// EndToEndClusterStage runs the same suite as EndToEndCluster, but stops at the boundary
// phase/60 draws: it stages the files and renders the engine config without starting the
// engine on any node. It takes a few minutes rather than tens of them and brings up no k3s
// cluster, which is what makes it reliable on a free hosted runner and why it is the one CI
// runs. Use EndToEndCluster for the walk that bootstraps.
func (Test) EndToEndClusterStage() error {
	if err := stopBootlooseContainers(); err != nil {
		return err
	}
	if err := os.Setenv("CARGOSHIP_E2E_STAGE_ONLY", "1"); err != nil {
		return err
	}
	return runE2ENoBuild("30m", "github.com/colonel-byte/cargoship/test/e2e/cluster/...")
}

// CleanCluster removes the containers a bootloose cluster left behind. EndToEndCluster does
// this before it runs, so this target is for the run that was killed partway through and left
// its five nodes holding memory, or for looking at what a failed run left and then clearing it.
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
	return runE2ENoBuild("3h", "github.com/colonel-byte/cargoship/test/e2e/cluster/...")
}
