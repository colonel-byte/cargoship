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

	"github.com/colonel-byte/cargoship/magefiles/pkg/testrunner"
	"github.com/magefile/mage/mg"
)

type (
	Test mg.Namespace
)

// EndToEnd runs the whole e2e suite: both the cluster and non-cluster groups.
func (Test) EndToEnd() error {
	if err := testrunner.StopBootlooseContainers(); err != nil {
		return err
	}
	return testrunner.RunE2E("1h", "github.com/colonel-byte/cargoship/test/e2e/...")
}

// EndToEndNonCluster runs the group that needs no cluster: misc and package command groups.
func (Test) EndToEndNonCluster() error {
	return testrunner.RunE2E("30m", "github.com/colonel-byte/cargoship/test/e2e/noncluster/...", "-short")
}

// EndToEndCluster runs the full install-command-group suite.
func (Test) EndToEndCluster() error {
	if err := testrunner.StopBootlooseContainers(); err != nil {
		return err
	}
	return testrunner.RunE2ENoBuild("1h", "github.com/colonel-byte/cargoship/test/e2e/cluster/...")
}

// EndToEndClusterStage runs the cluster suite stopping at the pre-engine staging boundary.
func (Test) EndToEndClusterStage() error {
	if err := testrunner.StopBootlooseContainers(); err != nil {
		return err
	}
	if err := os.Setenv("CARGOSHIP_E2E_STAGE_ONLY", "1"); err != nil {
		return err
	}
	return testrunner.RunE2ENoBuild("30m", "github.com/colonel-byte/cargoship/test/e2e/cluster/...")
}

// EndToEndClusterDryRun runs only the dry-run walk, which reports over nodes with nothing on
// them and asserts it left them that way. It starts no engine, so it runs against the smaller
// staging cluster and is the fastest way to exercise --dry-run against real hosts.
func (Test) EndToEndClusterDryRun() error {
	if err := testrunner.StopBootlooseContainers(); err != nil {
		return err
	}
	if err := os.Setenv("CARGOSHIP_E2E_STAGE_ONLY", "1"); err != nil {
		return err
	}
	return testrunner.RunE2ENoBuild(
		"30m",
		"github.com/colonel-byte/cargoship/test/e2e/cluster/...",
		"-run", "TestClusterPhases/dryrun",
	)
}

// EndToEndZarf runs the zarf Ansible module suite: two k3d clusters, a real zarf, and the
// uds-bundle's packages walked by the modules. Needs k3d, zarf, ansible-playbook and Docker.
func (Test) EndToEndZarf() error {
	if err := testrunner.DeleteK3dClusters(); err != nil {
		return err
	}
	return testrunner.RunE2EZarf()
}

// CleanZarfClusters removes the k3d clusters left behind by an interrupted zarf module suite.
func (Test) CleanZarfClusters() error {
	return testrunner.DeleteK3dClusters()
}

// CleanCluster removes containers left behind by bootloose.
func (Test) CleanCluster() error {
	return testrunner.StopBootlooseContainers()
}

// EndToEndClusterUpgrade runs the cluster suite with the upgrade walk turned on.
func (Test) EndToEndClusterUpgrade() error {
	if err := testrunner.StopBootlooseContainers(); err != nil {
		return err
	}
	if err := os.Setenv("CARGOSHIP_E2E_UPGRADE", "1"); err != nil {
		return err
	}
	return testrunner.RunE2ENoBuild("3h", "github.com/colonel-byte/cargoship/test/e2e/cluster/...")
}

// Unit runs every test suite but the e2e groups, which have runners of their own.
func (Test) Unit() error {
	return testrunner.Unit()
}

// Fuzz replays the fuzz seed corpus.
func (Test) Fuzz() error {
	return testrunner.Fuzz()
}
