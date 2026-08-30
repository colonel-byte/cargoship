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

// Package noncluster holds the e2e tests that drive the built binary without a cluster:
// the misc and package command groups. Nothing here starts a container or talks to a real
// registry, so the suite needs only the binary under build/ and is cheap enough to gate
// every pull request. The install group lives in the sibling cluster package.
package noncluster

import (
	"log"
	"os"
	"testing"

	"github.com/colonel-byte/cargoship/src/test"
)

var e2e test.CargoE2ETest //nolint:gochecknoglobals

func TestMain(m *testing.M) {
	var err error
	e2e, _, err = test.Bootstrap()
	if err != nil {
		log.Fatal(err)
	}

	code := m.Run()

	if minimalPkgDir != "" {
		if err := os.RemoveAll(minimalPkgDir); err != nil {
			log.Printf("failed to remove %s: %v", minimalPkgDir, err)
		}
	}
	os.Exit(code)
}
