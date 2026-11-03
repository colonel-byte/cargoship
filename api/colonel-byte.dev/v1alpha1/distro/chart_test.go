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

	"github.com/k0sproject/dig"
	"github.com/stretchr/testify/require"
)

func TestChartManifests(t *testing.T) {
	for _, tt := range []struct {
		name  string
		entry any
		want  ChartManifest
	}{
		{
			name:  "bare string is the values, chart enabled",
			entry: "kubeProxyReplacement: true\n",
			want: ChartManifest{
				Enabled: true,
				Values:  "kubeProxyReplacement: true\n",
			},
		},
		{
			name: "bare mapping is the values, chart enabled",
			entry: dig.Mapping{
				"encryption": dig.Mapping{"enabled": true},
			},
			want: ChartManifest{
				Enabled: true,
				Values: map[string]any{
					"encryption": dig.Mapping{"enabled": true},
				},
			},
		},
		{
			name: "a bare mapping whose values happen to use the wrapper key names",
			entry: dig.Mapping{
				"enabled": false,
				"images":  []any{"docker.io/rancher/mirrored-cilium-cilium:v1.20.2"},
			},
			want: ChartManifest{
				Enabled: true,
				Values: map[string]any{
					"enabled": false,
					"images":  []any{"docker.io/rancher/mirrored-cilium-cilium:v1.20.2"},
				},
			},
		},
		{
			name: "wrapper",
			entry: dig.Mapping{
				"enabled": false,
				"images":  []any{"docker.io/rancher/mirrored-cilium-cilium:v1.20.2"},
				"values":  "kubeProxyReplacement: true\n",
			},
			want: ChartManifest{
				Enabled: false,
				Images:  []string{"docker.io/rancher/mirrored-cilium-cilium:v1.20.2"},
				Values:  "kubeProxyReplacement: true\n",
			},
		},
		{
			name: "wrapper without enabled is enabled",
			entry: dig.Mapping{
				"values": dig.Mapping{"kubeProxyReplacement": true},
			},
			want: ChartManifest{
				Enabled: true,
				Values:  dig.Mapping{"kubeProxyReplacement": true},
			},
		},
		{
			name: "wrapper with enabled as an unretyped string",
			entry: dig.Mapping{
				"enabled": "false",
				"values":  "a: b\n",
			},
			want: ChartManifest{
				Enabled: false,
				Values:  "a: b\n",
			},
		},
		{
			name: "wrapper with an empty values mapping is still a wrapper",
			entry: dig.Mapping{
				"enabled": false,
				"values":  dig.Mapping{},
			},
			want: ChartManifest{
				Enabled: false,
				Values:  dig.Mapping{},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := ChartManifests(dig.Mapping{"manifest": dig.Mapping{"rke2-cilium": tt.entry}})
			require.Len(t, got, 1)
			require.Equal(t, tt.want, got["rke2-cilium"])
		})
	}
}

func TestChartManifestsAbsent(t *testing.T) {
	t.Run("no manifest section", func(t *testing.T) {
		engine := dig.Mapping{"config": dig.Mapping{"profile": "cis"}}
		require.Nil(t, ChartManifests(engine))
		// DigMapping would have written an empty manifest mapping into the engine.
		require.NotContains(t, engine, "manifest")
	})

	t.Run("empty manifest section", func(t *testing.T) {
		require.Nil(t, ChartManifests(dig.Mapping{"manifest": dig.Mapping{}}))
	})

	t.Run("manifest section is not a mapping", func(t *testing.T) {
		require.Nil(t, ChartManifests(dig.Mapping{"manifest": "nonsense"}))
	})

	t.Run("nil engine", func(t *testing.T) {
		require.Nil(t, ChartManifests(nil))
	})
}

func TestChartManifestsMapShapes(t *testing.T) {
	// The engine mapping is free-form and reaches this code from YAML, from the
	// values walker, and from Go, so all three map shapes have to read the same.
	want := ChartManifest{
		Enabled: false,
		Images:  []string{"example.com/one:1"},
		Values:  "a: b\n",
	}

	t.Run("map[string]any", func(t *testing.T) {
		got := ChartManifests(dig.Mapping{"manifest": map[string]any{
			"chart": map[string]any{
				"enabled": false,
				"images":  []any{"example.com/one:1"},
				"values":  "a: b\n",
			},
		}})
		require.Equal(t, want, got["chart"])
	})

	t.Run("map[any]any", func(t *testing.T) {
		got := ChartManifests(dig.Mapping{"manifest": map[any]any{
			"chart": map[any]any{
				"enabled": false,
				"images":  []any{"example.com/one:1"},
				"values":  "a: b\n",
			},
		}})
		require.Equal(t, want, got["chart"])
	})

	t.Run("images already a string slice", func(t *testing.T) {
		got := ChartManifests(dig.Mapping{"manifest": dig.Mapping{
			"chart": dig.Mapping{
				"enabled": false,
				"images":  []string{"example.com/one:1"},
				"values":  "a: b\n",
			},
		}})
		require.Equal(t, want, got["chart"])
	})
}

func TestConfigImages(t *testing.T) {
	cfg := Config{
		ImagesConfig: ImageConfig{
			Images: []string{"example.com/base:1", "example.com/shared:1"},
		},
		Engine: dig.Mapping{"manifest": dig.Mapping{
			// Deliberately not in sorted order: the output has to be.
			"rke2-cilium": dig.Mapping{
				"images": []any{"example.com/cilium:1", "example.com/shared:1"},
				"values": "a: b\n",
			},
			"rancher-vsphere-csi": dig.Mapping{
				"enabled": false,
				"images":  []any{"example.com/csi:1"},
				"values":  "a: b\n",
			},
		}},
	}

	require.Equal(t, []string{
		"example.com/base:1",
		"example.com/shared:1",
		"example.com/csi:1",
		"example.com/cilium:1",
	}, cfg.AllImages(), "every chart's images, charts in sorted order, deduplicated")

	require.Equal(t, []string{
		"example.com/base:1",
		"example.com/shared:1",
		"example.com/cilium:1",
	}, cfg.EnabledImages(), "a disabled chart's images are in the package but never uploaded")
}

func TestConfigImagesWithoutChartImages(t *testing.T) {
	// The shape every package carries today: both accessors have to return the
	// declared list untouched, or layer 1 is not the no-op it claims to be.
	cfg := Config{
		ImagesConfig: ImageConfig{
			Images: []string{"example.com/b:1", "example.com/a:1"},
		},
		Engine: dig.Mapping{"manifest": dig.Mapping{
			"rke2-cilium": "kubeProxyReplacement: true\n",
		}},
	}

	require.Equal(t, cfg.ImagesConfig.Images, cfg.AllImages())
	require.Equal(t, cfg.ImagesConfig.Images, cfg.EnabledImages())
}

func TestConfigImagesNoManifest(t *testing.T) {
	cfg := Config{ImagesConfig: ImageConfig{Images: []string{"example.com/a:1"}}}

	require.Equal(t, []string{"example.com/a:1"}, cfg.AllImages())
	require.Equal(t, []string{"example.com/a:1"}, cfg.EnabledImages())
	require.Nil(t, Config{}.AllImages())
}

// An image two charts both need is uploaded while either of them is enabled, and carried
// once. Upstream airgap image lists do not divide along chart lines -- rke2's vSphere list
// holds the CPI image and the CSI driver together, and hardened-cni-plugins is in both the
// cilium and the vSphere lists -- so a shared image has to be declarable under every chart
// that needs it without being counted twice or withheld by the wrong one.
func TestConfigImagesSharedBetweenCharts(t *testing.T) {
	shared := "example.com/hardened-cni-plugins:1"
	cfg := func(cpi, csi bool) Config {
		return Config{
			Engine: dig.Mapping{"manifest": dig.Mapping{
				"rancher-vsphere-cpi": dig.Mapping{
					"enabled": cpi,
					"images":  []any{shared, "example.com/cpi:1"},
					"values":  "a: b\n",
				},
				"rancher-vsphere-csi": dig.Mapping{
					"enabled": csi,
					"images":  []any{shared, "example.com/csi:1"},
					"values":  "a: b\n",
				},
			}},
		}
	}

	require.Equal(t, []string{shared, "example.com/cpi:1", "example.com/csi:1"},
		cfg(true, true).AllImages(), "carried once, whatever enablement says")

	require.Equal(t, []string{shared, "example.com/cpi:1"},
		cfg(true, false).EnabledImages(), "the shared image follows the chart that is still enabled")

	require.Equal(t, []string{shared, "example.com/csi:1"},
		cfg(false, true).EnabledImages())

	require.Nil(t, cfg(false, false).EnabledImages(), "neither chart enabled uploads neither chart's images")
}
