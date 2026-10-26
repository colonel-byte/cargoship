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
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/helpers"
	"github.com/k0sproject/dig"
)

// ChartManifest is one entry of the engine's manifest section: the Helm values
// cargoship writes to that chart's HelmChartConfig, and the images the chart needs.
type ChartManifest struct {
	// Enabled reports whether the chart's images are carried onto hosts. An entry
	// that does not say is enabled.
	Enabled bool
	// Images lists the offline images this chart needs, carried in the package and
	// uploaded only while the chart is enabled.
	Images []string
	// Values is the chart's Helm values: a YAML string, which is passed through as
	// the values file's contents, or a mapping, which is marshalled back to YAML.
	Values any
}

// ChartManifests reads `.spec.config.engine.manifest` out of an engine mapping. It
// takes the mapping rather than a Config so that a package's own spec and a mapping
// already resolved for one node are both readable, and it never writes to it --
// dig's DigMapping inserts an empty mapping for a key that is absent, which would
// add a bare `manifest:` to a package that has none.
func ChartManifests(engine dig.Mapping) map[string]ChartManifest {
	entries, ok := asMapping(engine[config.EngineManifest])
	if !ok || len(entries) == 0 {
		return nil
	}

	out := make(map[string]ChartManifest, len(entries))
	for chart, entry := range entries {
		out[chart] = chartManifest(entry)
	}
	return out
}

// chartManifest reads one manifest entry, in either of the two shapes the section
// accepts.
//
// An entry written before the wrapper existed is the chart's Helm values itself: a
// YAML block string, or a mapping. Both are read as Values with the chart enabled,
// which is what every package in the wild carries today.
//
// The discriminator is the `values` key alone. A chart's own values may well have a
// top-level `enabled` or `images` key -- cilium has `enablePolicy`, and
// rancher-vsphere-csi really does take an `images` list -- so neither of those can
// tell the two shapes apart. The cost is that a chart whose values have a top-level
// `values` key cannot use the wrapper and has to stay in the bare form.
//
// TODO(v1beta1): drop this conversion. v1beta1 carries the wrapper and nothing else,
// and converting between the two API versions is where this belongs instead of here.
func chartManifest(entry any) ChartManifest {
	m, ok := asMapping(entry)
	if !ok {
		// A string, or anything else helmValuesContent can marshal.
		return ChartManifest{Enabled: true, Values: entry}
	}
	raw, wrapped := m[chartKeyValues]
	if !wrapped {
		return ChartManifest{Enabled: true, Values: m}
	}

	return ChartManifest{
		Enabled: chartEnabled(m[chartKeyEnabled]),
		Images:  stringList(m[chartKeyImages]),
		Values:  raw,
	}
}

// Keys of a manifest wrapper entry.
const (
	chartKeyEnabled = "enabled"
	chartKeyImages  = "images"
	chartKeyValues  = "values"
)

// chartEnabled reads a wrapper's enabled key. A string is accepted because a value
// can reach the key through a mapping without having been retyped on the way.
//
// Anything else counts as enabled, which is the safe direction: an image carried to
// a host that does not need it wastes a transfer, while one withheld from a host
// that does leaves a pod stuck pulling from a registry it cannot reach.
func chartEnabled(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		enabled, err := strconv.ParseBool(b)
		return err != nil || enabled
	default:
		return true
	}
}

// stringList reads a YAML sequence of strings out of a free-form mapping. An entry
// that is not a string is formatted as one, the way an image reference written as a
// bare number or a date would otherwise be dropped without a word.
func stringList(v any) []string {
	items, ok := v.([]any)
	if !ok {
		if s, isSlice := v.([]string); isSlice {
			return s
		}
		return nil
	}

	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, isString := item.(string); isString {
			out = append(out, s)
			continue
		}
		out = append(out, fmt.Sprint(item))
	}
	return out
}

// asMapping reads a value as a string-keyed mapping. All three shapes occur: YAML
// decoded through dig gives a Mapping, the values walker writes plain maps before
// Dup restores them, and a mapping built in Go may be either.
func asMapping(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case dig.Mapping:
		return m, true
	case map[string]any:
		return m, true
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, item := range m {
			out[fmt.Sprint(k)] = item
		}
		return out, true
	}
	return nil, false
}

// AllImages returns every image the package carries: imageConfig.images, plus the
// images of every chart manifest whether or not it is enabled. This is the set that
// has to be pulled at create time and enumerated when a package's layers are
// collected -- LayersFromImages fails on a declared image whose blobs are absent, so
// a chart's images must be in the package even when no cluster will be sent them.
func (c Config) AllImages() []string {
	return c.images(false)
}

// EnabledImages returns the images that reach a host: imageConfig.images, plus the
// images of the chart manifests that are enabled. A disabled chart's images stay in
// the package and are not exported or uploaded.
func (c Config) EnabledImages() []string {
	return c.images(true)
}

func (c Config) images(enabledOnly bool) []string {
	manifests := ChartManifests(c.Engine)
	if len(manifests) == 0 {
		return c.ImagesConfig.Images
	}

	out := slices.Clone(c.ImagesConfig.Images)
	// Sorted, because the order of this list reaches the published layer list and
	// two builds from the same inputs have to produce the same digests.
	for _, chart := range slices.Sorted(maps.Keys(manifests)) {
		m := manifests[chart]
		if enabledOnly && !m.Enabled {
			continue
		}
		out = append(out, m.Images...)
	}
	return helpers.Unique(out)
}
