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
	"os"
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/test"
	"github.com/stretchr/testify/require"
)

// TestContainerSafeDefinitionKeepsOutsideSourcesResolvable resolves every relative source in the
// copied manifest the way fileGrabber resolves a non-URL source, so a definition that reaches
// outside its own directory cannot quietly lose those files again.
//
// It is worth its own test because the failure it guards against was invisible for as long as it
// existed: the k3s walks built a package with no killall.sh and neither systemd unit, the staging
// errors were logged and skipped, the upload of the three missing files failed just as quietly,
// and the suite passed throughout.
func TestContainerSafeDefinitionKeepsOutsideSourcesResolvable(t *testing.T) {
	for _, def := range exampleDefinitions {
		t.Run(def.path, func(t *testing.T) {
			// TestMain has already chdir'd to the repo root, which is what the relative
			// paths in exampleDefinitions and exampleDir are both written against.
			definition, err := test.ContainerSafeDefinition(def.path, t.TempDir())
			require.NoError(t, err)

			content, err := os.ReadFile(filepath.Join(definition, "distro.yaml")) //nolint:gosec
			require.NoError(t, err)

			for _, match := range test.OutsideSource.FindAllSubmatch(content, -1) {
				source := string(match[1])
				resolved := filepath.Join(definition, source)
				_, err := os.Stat(resolved)
				require.NoError(t, err, "source %q resolves to %q, which is not in the copy", source, resolved)
			}
		})
	}
}
