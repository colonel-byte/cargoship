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

// EndToEnd runs the whole e2e suite: both the cluster and non-cluster groups, including the
// example packages that pull ~1.5GB of engine artifacts and images. Needs Docker.
func (Test) EndToEnd() error {
	if err := stopBootlooseContainers(); err != nil {
		return err
	}
	return runE2E("1h", "github.com/colonel-byte/cargoship/src/test/e2e/...")
}
