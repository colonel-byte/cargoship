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

package distro

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func chartConfig() Config {
	return Config{
		Charts: []Chart{
			{
				Name:   "my-app",
				Source: "charts/my-app-1.2.3.tgz",
				Shasum: "abc123",
				When:   ".myApp.enabled",
			},
		},
	}
}

func TestValidateChartsRefusals(t *testing.T) {
	for _, tt := range []struct {
		name       string
		distroType string
		charts     []Chart
		want       string
	}{
		{
			name:       "a distro with no helm controller",
			distroType: "upstream",
			charts:     chartConfig().Charts,
			want:       "no helm controller",
		},
		{
			name:       "two charts of one name",
			distroType: "rke2",
			charts: []Chart{
				{
					Name:   "my-app",
					Source: "charts/a.tgz",
				},
				{
					Name:   "my-app",
					Source: "charts/b.tgz",
				},
			},
			want: "declared twice",
		},
		{
			name:       "a chart with no source",
			distroType: "rke2",
			charts: []Chart{
				{
					Name: "my-app",
				},
			},
			want: "has no source",
		},
		{
			name:       "a chart with no name",
			distroType: "rke2",
			charts: []Chart{
				{
					Source: "charts/a.tgz",
				},
			},
			want: "has no name",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := Config{Charts: tt.charts}
			require.ErrorContains(t, c.ValidateCharts(tt.distroType), tt.want)
		})
	}
}

// A package with no charts is passed over, including by the distro-type check: upstream
// packages are the majority and none of them declare one.
func TestValidateChartsIgnoresAPackageWithNone(t *testing.T) {
	c := Config{}
	require.NoError(t, c.ValidateCharts("upstream"))
}

func TestResolveChartDelivery(t *testing.T) {
	c := Config{
		Charts: []Chart{
			{
				Name: "gated-on",
				When: ".on",
			},
			{
				Name: "gated-off",
				When: ".off",
			},
			{
				Name: "ungated",
			},
		},
	}
	c.ResolveChartDelivery(func(path string) bool { return path == ".on" })

	var delivered []string
	for _, chart := range c.DeliveredCharts() {
		delivered = append(delivered, chart.Name)
	}
	require.Equal(t, []string{"gated-on", "ungated"}, delivered)
}

func TestChartManifestName(t *testing.T) {
	require.Equal(t, "my-app-chart.yaml", Chart{Name: "my-app"}.ManifestName())
}

// The projection is what the three directory walkers read a chart as -- the build that stages
// it, the layer list a published package is pulled by, and the mirror. It is not carried in the
// definition, because a real file entry would have the chart uploaded to every controller for
// nothing.
func TestChartFiles(t *testing.T) {
	files := ChartFiles([]Chart{
		{
			Name:   "my-app",
			Source: "charts/my-app-1.2.3.tgz",
			Shasum: "abc123",
		},
	})

	require.Len(t, files, 1)
	require.Equal(t, "charts/my-app-1.2.3.tgz", files[0].Source)
	require.Equal(t, "abc123", files[0].Shasum)
	require.Equal(t, "/my-app-1.2.3.tgz", files[0].Target, "the target only names the staged path")
}

// A chart's own images have to be in the package, or the chart installs and its pods sit
// pulling from a registry the cluster cannot reach -- which is the failure this whole section
// exists to avoid. They were declared and silently dropped until the image list learned about
// them.
func TestChartImagesAreCarried(t *testing.T) {
	c := Config{
		Charts: []Chart{
			{
				Name:   "my-app",
				Source: "charts/my-app-1.2.3.tgz",
				Images: []string{"registry.example.com/my-app:1.2.3"},
			},
		},
	}
	c.ImagesConfig.Images = []string{"registry.k8s.io/pause:3.10"}

	require.Equal(t, []string{"registry.k8s.io/pause:3.10", "registry.example.com/my-app:1.2.3"}, c.AllImages())
	require.Equal(t, c.AllImages(), c.EnabledImages(), "a chart's When gate cannot be answered here, so both lists carry them")
}
