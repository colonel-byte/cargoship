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

package examples

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/internal/cfg"
	"github.com/stretchr/testify/require"
)

// The rendered example is decoded the way an install decodes it, which is what proves the
// definition the templates produce is one cargoship accepts -- the chart's own validation runs
// in the decoder, so a flavor that rendered a chart this engine cannot install, or one missing
// a digest its source needs, fails here rather than on a management node.
//
// A flavor with nothing rendered yet is skipped rather than failed, so this test lives with the
// templates and the API it checks rather than one layer up with the output: the renders land in
// their own pull request, and a test that required them could not be reviewed beside the code
// that produces them.
func TestRenderedChartExampleDecodes(t *testing.T) {
	var paths []string
	for _, dir := range []string{"k3s-chart-http", "k3s-chart-oci"} {
		found, err := filepath.Glob(filepath.Join(repoRoot, "example", dir, "*", "*", "distro.yaml"))
		require.NoError(t, err)
		paths = append(paths, found...)
	}
	if len(paths) == 0 {
		t.Skip("no chart flavor is rendered yet; the renders land in their own layer")
	}

	for _, p := range paths {
		t.Run(filepath.Join(filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(p)))), filepath.Base(filepath.Dir(p))), func(t *testing.T) {
			b, err := os.ReadFile(filepath.Clean(p))
			require.NoError(t, err)

			dis, err := cfg.Parse(context.Background(), b)
			require.NoError(t, err)

			require.Len(t, dis.Spec.Config.Charts, 1)
			chart := dis.Spec.Config.Charts[0]
			require.Equal(t, ".chart.enabled", chart.When)
			require.NotEmpty(t, chart.TarballName())
			// Every chart these flavors carry is fetched, so each one has to name a
			// digest or be pinned by one -- which is what the decoder refuses on.
			require.True(t, chart.Shasum != "" || strings.Contains(chart.Source, "@sha256:"),
				"a fetched chart with neither a shasum nor a digest in its reference")
			// Not a file entry: a chart reaches the cluster inside its resource, so a
			// package that uploaded one to every controller would be carrying it twice.
			for _, f := range dis.Spec.Config.Files {
				require.NotContains(t, f.Target, chart.TarballName())
			}
		})
	}
}
