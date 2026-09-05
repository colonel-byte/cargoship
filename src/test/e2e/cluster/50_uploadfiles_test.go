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

package cluster

import (
	"strings"

	apicluster "github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/pkg/phase"
)

// uploadManifestPath is where the upload phases record what they put on a host. The phase
// package keeps the constant unexported, so the path is repeated here on purpose: if it ever
// moves, this test should fail rather than quietly stop checking anything.
const uploadManifestPath = "/var/lib/cargoship/manifest.txt"

// Test_50_UploadFiles covers phase/50_uploadfiles.go. It stages the package's images and
// files on the nodes and records each one in the upload manifest, so the assertion is that
// the manifest exists, is not empty, and that every path it claims is really on the host.
func (s *ApplyPhaseSuite) Test_50_UploadFiles() {
	p := &phase.UploadFiles{}
	s.runPhase(p)
	s.Require().True(ran(p), "nothing to upload from a package that carries images")

	for _, host := range s.harness.hosts() {
		entries := s.manifestOn(host)
		s.Require().NotEmptyf(entries, "%s: upload manifest is empty", host)

		for _, path := range entries {
			s.Require().Truef(host.FileExist(path),
				"%s: manifest claims %s but it is not on the host", host, path)
		}
	}
}

// manifestOn returns the paths the upload manifest on host records, one per line as
// "category\tpath".
func (s *ApplyPhaseSuite) manifestOn(host *apicluster.ZarfHost) []string {
	s.T().Helper()

	if !host.FileExist(uploadManifestPath) {
		return nil
	}
	content, err := host.ReadFile(uploadManifestPath)
	s.Require().NoError(err)

	var paths []string
	for _, line := range strings.Split(content, "\n") {
		_, path, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok || path == "" {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}
