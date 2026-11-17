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

package distrocfg

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/base64"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// The whole point of chartContent is that what the cluster receives is a chart archive, so this
// takes the written manifest apart the way the helm controller does: parse the YAML, base64
// decode the field, and read a Chart.yaml out of the gzipped tar. A mistake anywhere in the
// chain -- a wrong encoding, a truncated read, YAML folding the line -- survives every other
// test in this package and fails here.
func TestHelmChartContentIsAChartArchive(t *testing.T) {
	tarball := chartArchive(t)

	d := newTestRancher()
	dis := distro.Distro{}
	dis.Spec.Config.Charts = []distro.Chart{
		{
			Name:    "hello",
			Source:  "charts/hello-0.1.0.tgz",
			Deliver: true,
			Content: tarball,
		},
	}

	cfg := &fakeHost{fileExist: map[string]bool{}}
	host := cfg.attach(&cluster.Host{Role: cluster.RoleController, Hostname: "node1"})
	host.Metadata.IsLeader = true
	require.NoError(t, d.ConfigureEngine(context.Background(), host, cluster.RuntimeMeta{}, dis))

	written, ok := cfg.files[filepath.Join(d.Data, "server/manifests/hello-chart.yaml")]
	require.True(t, ok, "no HelmChart manifest was written")

	var manifest struct {
		Spec struct {
			ChartContent string `yaml:"chartContent"`
		} `yaml:"spec"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(written), &manifest))

	decoded, err := base64.StdEncoding.DecodeString(manifest.Spec.ChartContent)
	require.NoError(t, err, "chartContent is not base64 the helm controller can decode")
	require.Equal(t, tarball, decoded, "the chart that reaches the cluster is not the chart the package carried")

	gz, err := gzip.NewReader(strings.NewReader(string(decoded)))
	require.NoError(t, err)
	h, err := tar.NewReader(gz).Next()
	require.NoError(t, err)
	require.Equal(t, "hello/Chart.yaml", h.Name)
}

// chartArchive is a minimal chart archive: one Chart.yaml under the chart's own directory,
// which is the shape helm requires of an archive it installs.
func chartArchive(t *testing.T) []byte {
	t.Helper()

	var buf strings.Builder
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := "apiVersion: v2\nname: hello\nversion: 0.1.0\n"
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: "hello/Chart.yaml",
		Mode: 0o644,
		Size: int64(len(body)),
	}))
	_, err := io.WriteString(tw, body)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())

	return []byte(buf.String())
}
