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
	"context"
	"encoding/base64"
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/stretchr/testify/require"
)

func chartDistro() distro.Distro {
	dis := distro.Distro{}
	dis.Spec.Config.Charts = []distro.Chart{
		{
			Name:            "my-app",
			Source:          "charts/my-app-1.2.3.tgz",
			TargetNamespace: "my-app",
			CreateNamespace: true,
			Values:          "replicas: 2",
			Deliver:         true,
			Content:         []byte("a chart tarball"),
		},
	}
	return dis
}

// The chart travels inside the resource, base64-encoded, rather than being uploaded to the node
// and pointed at -- which is the helm controller's own airgap mechanism and the reason a chart
// needs nothing on a host's filesystem.
func TestConfigureEngineWritesAHelmChart(t *testing.T) {
	d := newTestRancher()
	cfg := &fakeHost{fileExist: map[string]bool{}}
	host := cfg.attach(&cluster.Host{Role: cluster.RoleController, Hostname: "node1"})
	host.Metadata.IsLeader = true

	require.NoError(t, d.ConfigureEngine(context.Background(), host, cluster.RuntimeMeta{}, chartDistro()))

	path := filepath.Join(d.Data, "server/manifests/my-app-chart.yaml")
	got := parseWrittenYAML(t, cfg.files, path)
	require.Equal(t, "HelmChart", got.DigString(keyKind))
	require.Equal(t, "helm.cattle.io/v1", got.DigString(keyAPIVersion))
	require.Equal(t, "my-app", got.DigString(keyMetadata, "name"))
	// The resource lives where the helm controller watches, whatever the release installs into.
	require.Equal(t, "kube-system", got.DigString(keyMetadata, "namespace"))
	require.Equal(t, base64.StdEncoding.EncodeToString([]byte("a chart tarball")), got.DigString(keySpec, "chartContent"))
	require.Empty(t, got.DigString(keySpec, "chart"), "nothing points at a repository or a served path")
	require.Equal(t, "my-app", got.DigString(keySpec, "targetNamespace"))
	require.Equal(t, "replicas: 2", got.DigString(keySpec, "valuesContent"))
}

// A worker has no manifest directory, so the chart is a controller-only file the same way a
// HelmChartConfig is.
func TestHelmChartIsControllerOnly(t *testing.T) {
	d := newTestRancher()
	cfg := &fakeHost{fileExist: map[string]bool{}}
	host := cfg.attach(&cluster.Host{Role: cluster.RoleWorker, Hostname: "node1"})

	require.NoError(t, d.ConfigureEngine(context.Background(), host, cluster.RuntimeMeta{}, chartDistro()))

	require.NotContains(t, cfg.files, filepath.Join(d.Data, "server/manifests/my-app-chart.yaml"))
}

// A chart a cluster gated off is left out of the desired set, which is also what gets one
// written by an earlier run removed -- and so uninstalled by the helm controller.
func TestHelmChartHeldBackByItsGate(t *testing.T) {
	d := newTestRancher()
	dis := chartDistro()
	dis.Spec.Config.Charts[0].Deliver = false
	cfg := &fakeHost{fileExist: map[string]bool{}}
	host := cfg.attach(&cluster.Host{Role: cluster.RoleController, Hostname: "node1"})
	host.Metadata.IsLeader = true

	require.NoError(t, d.ConfigureEngine(context.Background(), host, cluster.RuntimeMeta{}, dis))

	require.NotContains(t, cfg.files, filepath.Join(d.Data, "server/manifests/my-app-chart.yaml"))
}

// A chart whose bytes were never read would be written as a resource with an empty
// chartContent, which the helm controller answers by failing an install job on the cluster. The
// writer refuses instead, naming the chart.
func TestHelmChartRefusesEmptyContent(t *testing.T) {
	d := newTestRancher()
	dis := chartDistro()
	dis.Spec.Config.Charts[0].Content = nil
	cfg := &fakeHost{fileExist: map[string]bool{}}
	host := cfg.attach(&cluster.Host{Role: cluster.RoleController, Hostname: "node1"})
	host.Metadata.IsLeader = true

	// DesiredFiles rather than ConfigureEngine: ConfigureEngine warns and writes nothing on a
	// render failure, the same way it does for every other file it renders, so the error is
	// only returned to the engine-config-sync phase that checks it.
	_, err := d.DesiredFiles(context.Background(), host, cluster.RuntimeMeta{}, dis)
	require.ErrorContains(t, err, "carries no content")
}
