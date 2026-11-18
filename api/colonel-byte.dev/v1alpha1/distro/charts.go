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
	"path"
	"regexp"
	"strings"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/pkg/helpers"
	"github.com/invopop/jsonschema"
)

// Chart is one Helm chart the package carries and deploys, as distinct from
// `.spec.config.engine.manifest`, which configures a chart the engine already bundles.
//
// The two are separate sections, and deliberately, because an entry in each has a different
// blast radius. A manifest entry writes a HelmChartConfig: dropping it stops configuring a
// release somebody else owns. A chart entry writes a HelmChart, which *owns* the release -- so
// dropping a chart entry deletes the resource, and the engine's helm controller answers that by
// uninstalling the workload. Two things with opposite consequences for the same edit do not
// belong in one map keyed the same way.
//
// Only the Rancher engines install these: k3s and rke2 both run the helm controller, and a
// distro without one ignores the section the way it ignores Config.Manifests.
//
// The chart travels inside the resource, base64-encoded in its chartContent, which is the helm
// controller's own airgap mechanism. Nothing is uploaded to a host -- the bytes go to the API
// server with the manifest cargoship writes and the install job reads them from there -- so a
// chart is the one thing a package delivers that never lands on a node's filesystem.
type Chart struct {
	// Name is the HelmChart resource's name, and the stem of the manifest file cargoship
	// writes for it. It is a Kubernetes object name, so it is bounded the way one is.
	Name string `json:"name" jsonschema:"pattern=^[a-z0-9]([a-z0-9\\-]*[a-z0-9])?$,maxLength=253"`
	// Source is where the chart tarball comes from: a path relative to the package
	// definition, an absolute path, an http(s) URL, or an `oci://` reference to a chart
	// published as an OCI artifact, which is how `helm push` publishes one.
	//
	// Whatever it names, what lands in the package is a `.tgz`, because that is what the
	// helm controller installs from on a host that can reach no chart repository. A `repo:`
	// and `version:` pair is deliberately not accepted: it would have the cluster resolve
	// the chart, which is the one thing an air-gapped cluster cannot do.
	Source string `json:"source" jsonschema:"example=charts/my-app-1.2.3.tgz,example=https://example.com/charts/my-app-1.2.3.tgz,example=oci://registry.example.com/charts/my-app:1.2.3"`
	// Version is the chart version, used to name the tarball when Source does not -- an
	// `oci://` reference names a repository and a tag, not a file. It is required for such a
	// source and ignored for one that already ends in a tarball name.
	Version string `json:"version,omitempty" jsonschema:"example=1.2.3"`
	// Shasum is the SHA-256 the tarball is verified against when the package is built.
	//
	// Required for a source that is fetched, and optional for a local file. The asymmetry is
	// the same one --file-override and --content-store are held to: a local path is a file
	// the package author chose and can see, while bytes arriving over a network are bytes
	// nobody has looked at, and a chart installs into the cluster with whatever permissions
	// its templates ask for. An `oci://` source pinned by digest carries its own guarantee
	// and needs no shasum.
	Shasum string `json:"shasum,omitempty"`
	// TargetNamespace is the namespace the release is installed into. The helm controller
	// defaults it to the chart's own, which is usually not what an operator wants.
	TargetNamespace string `json:"targetNamespace,omitempty"`
	// CreateNamespace has the helm controller create TargetNamespace if it is absent.
	CreateNamespace bool `json:"createNamespace,omitempty"`
	// Values is the chart's Helm values, as a YAML string or a mapping. Cargoship writes it
	// to the HelmChart's valuesContent, and renders it as a Go template first, so a value a
	// cluster sets reaches the chart -- the same spelling `.spec.config.engine.manifest`
	// entries use.
	Values any `json:"values,omitempty"`
	// Images lists the images the chart needs, carried in the package alongside
	// imageConfig.images.
	//
	// shortcut: these are carried and uploaded whether or not When gates the chart off,
	// because the flat image list has no per-value gate to hang them on -- the same gap
	// docs/agent/todo-chart-disable-image-gating.md records for a disabled chart manifest.
	// Upgrade both together.
	Images []string `json:"images,omitempty" jsonschema:"uniqueItems=true"`
	// When is an optional values path (e.g. ".myApp.enabled") gating whether the chart is
	// delivered and deployed at all. An empty path means always.
	When string `json:"when,omitempty" jsonschema:"example=.myApp.enabled"`

	// Deliver reports whether this chart's When gate is satisfied by the cluster's resolved
	// values. Cargoship resolves it once per run, before any phase, because the writer that
	// renders the HelmChart manifest has the package but not the values. A chart with no
	// When gate is delivered, which is why the resolved value rather than the path is what
	// the writer reads.
	Deliver bool `json:"-"`

	// Content is the chart tarball itself, read out of the extracted package on the
	// management node before any phase runs. It is what the HelmChart resource carries, so
	// it is the one field the writer cannot do without.
	Content []byte `json:"-"`
}

// JSONSchemaExtend pins down the shape of a chart's values, which are either a YAML string or a
// mapping cargoship serializes to YAML -- the same two shapes an engine manifest entry accepts,
// and for the same reason: a chart's values are the chart's vocabulary, not cargoship's.
func (Chart) JSONSchemaExtend(s *jsonschema.Schema) {
	values, ok := s.Properties.Get("values")
	if !ok {
		return
	}
	values.Ref = ""
	values.Type = ""
	values.Description = "the Helm values cargoship writes to this chart's HelmChart, as a YAML string or a mapping. Rendered as a Go template against the package's values first."
	values.OneOf = []*jsonschema.Schema{
		{Type: "string"},
		{Type: "object"},
	}
}

// chartTarballSuffixes are the extensions a source is read as already naming its tarball.
var chartTarballSuffixes = []string{".tgz", ".tar.gz"}

// TarballName is the file name the chart's tarball is staged under inside the package.
//
// A source that already ends in a tarball name keeps it, which covers a local path and a URL
// and means what is in the package is named the way upstream published it. A source that does
// not -- an `oci://` reference names a repository and a tag -- is named the way helm names a
// packaged chart, `<name>-<version>.tgz`, which is why Version is required for one.
//
// Nothing on a cluster ever sees this name: the chart reaches the API server as bytes inside
// its resource. It is what a reviewer reading a built package, and the mirror re-publishing the
// chart's source, both address it by.
func (c Chart) TarballName() string {
	if chartSourceNamesATarball(c.Source) {
		return path.Base(strings.TrimSuffix(c.Source, "/"))
	}
	return fmt.Sprintf("%s-%s.tgz", c.Name, c.Version)
}

// chartSourceNamesATarball reports whether a source already ends in a chart archive's name.
func chartSourceNamesATarball(source string) bool {
	base := path.Base(strings.TrimSuffix(source, "/"))
	for _, suffix := range chartTarballSuffixes {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return false
}

// needsShasum reports whether this source has to declare one: anything fetched over a network,
// unless an oci:// reference already pins the bytes by digest.
func (c Chart) needsShasum() bool {
	switch {
	case helpers.IsOCIURL(c.Source):
		return !strings.Contains(c.Source, "@sha256:")
	case helpers.IsURL(c.Source):
		return true
	default:
		return false
	}
}

// ManifestName is the file cargoship writes the HelmChart resource to, inside the engine's
// manifest directory. The suffix is what tells cargoship's files apart from the engine's own
// bundled manifests in the same directory, and from the HelmChartConfig files cargoship writes
// there for bundled charts.
func (c Chart) ManifestName() string {
	return ChartManifestPrefix + c.Name + ChartManifestSuffix
}

// ChartManifestPrefix and ChartManifestSuffix bracket the name of every HelmChart manifest
// cargoship writes. Together they are also the glob the manifest directory is pruned with, so a
// chart dropped from a package has its manifest removed -- and the release uninstalled by the
// helm controller as a result.
//
// The prefix is what keeps that prune off files cargoship does not own. Targeting the engine's
// manifest directory with a spec.config.files entry is an established pattern, and such a file
// is not in DesiredFiles, so a package shipping its own cilium-chart.yaml there would have had
// it deleted and its release uninstalled on the next engine-config-sync.
const (
	ChartManifestPrefix = "cargoship-"
	ChartManifestSuffix = "-chart.yaml"
)

// chartEngines are the distro types whose engine runs a helm controller, and so can install a
// chart a package carries. The ids are repeated here rather than read from types/distrocfg, for
// the same reason pkg/packager/assemble repeats Upstream's: the check below runs while a
// definition is being decoded, long before a distro object exists, and importing the engine
// implementations into the API types to look up two strings would invert the dependency.
var chartEngines = map[string]bool{
	"rke2": true,
	"k3s":  true,
}

// MaxChartContentBytes is the tarball size above which a chart is reported as a problem rather
// than carried quietly.
//
// A chart reaches the cluster inside its HelmChart resource, base64-encoded, so it is stored in
// etcd -- which refuses a value over about 1.5MB by default, and base64 is a third larger than
// what it encodes. A chart near that line installs today and fails the day it grows, with an
// etcd error nobody connects to a package. It is a warning rather than a refusal, because the
// limit is the cluster's to raise and a package author may well know theirs is raised: see
// docs/guides/charts.md.
const MaxChartContentBytes = 1 << 20

// chartName is the same pattern the generated values schemas enforce on a chart's name: a
// HelmChart resource name, which is also the stem of the file written into the engine's
// manifest directory. Anchored, so nothing resolving to a path separator or a parent directory
// can reach filepath.Join.
var chartName = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-]*[a-z0-9])?$`)

// ValidateCharts checks what the schema cannot: that this distro can install a chart at all,
// that no two entries would write one resource, and that anything fetched names a digest.
//
// It runs when a definition is decoded, which is every path that reads one -- the build that
// stages the charts and the load of a built package that reads them back -- so a definition
// that cannot work is refused before anything is fetched.
func (c *Config) ValidateCharts(distroType string) error {
	if len(c.Charts) == 0 {
		return nil
	}
	if !chartEngines[distroType] {
		return fmt.Errorf("spec.config.charts is set, but %q has no helm controller to install them: charts are supported on rke2 and k3s", distroType)
	}

	manifests := ChartManifests(c.Engine)
	seen := make(map[string]bool, len(c.Charts))
	for i, chart := range c.Charts {
		if chart.Name == "" {
			return fmt.Errorf("spec.config.charts[%d] has no name", i)
		}
		// The name is joined onto the engine's manifest directory and written as root on
		// every controller, so it has to be checked here and not only by the JSON schema:
		// the schema's pattern is enforced on the build path, and a definition read back out
		// of a package that cargoship did not build never passes through it.
		if !chartName.MatchString(chart.Name) {
			return fmt.Errorf("chart %s is not a valid name: it becomes a HelmChart resource name and a file in the engine's manifest directory, so it must match %s",
				chart.Name, chartName)
		}
		// A chart whose name collides with one of the engine's own bundled addons, or with a
		// manifest entry this package writes, cannot install: the engine's deploy controller
		// tracks ownership per addon and refuses to apply an object another owns, so the
		// error lands on the cluster rather than here.
		if _, ok := manifests[chart.Name]; ok {
			return fmt.Errorf("chart %s is also declared under spec.config.engine.manifest: one name cannot be both a carried chart and a chart configuration", chart.Name)
		}
		if chart.Source == "" {
			return fmt.Errorf("chart %s has no source", chart.Name)
		}
		if seen[chart.Name] {
			return fmt.Errorf("chart %s is declared twice: the name is the HelmChart's own, so two entries would write one resource", chart.Name)
		}
		seen[chart.Name] = true

		// A source that does not name a tarball is named from the chart and its version, so
		// a missing version would stage a file called "my-app-.tgz". Caught here rather
		// than at the fetch, which would succeed.
		if chart.Version == "" && !chartSourceNamesATarball(chart.Source) {
			return fmt.Errorf("chart %s names no version, and its source %s does not name a tarball: one of the two has to say what the file is called",
				chart.Name, chart.Source)
		}
		if chart.needsShasum() && chart.Shasum == "" {
			return fmt.Errorf("chart %s is fetched from %s and declares no shasum: refusing to carry a chart nobody verified",
				chart.Name, chart.Source)
		}
	}

	return nil
}

// ResolveChartDelivery stamps each chart's Deliver from its When gate, using gate to read the
// cluster's resolved values. It is the one place the gate is evaluated: the writer that renders
// the HelmChart manifest sees the package and not the values.
func (c *Config) ResolveChartDelivery(gate func(path string) bool) {
	for i := range c.Charts {
		c.Charts[i].Deliver = c.Charts[i].When == "" || gate(c.Charts[i].When)
	}
}

// DeliveredCharts returns the charts whose gate is satisfied, in the order the package declares
// them.
func (c Config) DeliveredCharts() []Chart {
	var out []Chart
	for _, chart := range c.Charts {
		if chart.Deliver {
			out = append(out, chart)
		}
	}
	return out
}

// ChartFiles projects the charts onto file entries, in declaration order, so that the three
// places which walk a package's staged directories -- the build that stages them, the layer
// list a published package is pulled by, and the mirror that re-publishes their sources -- can
// treat a chart tarball as the file it is on disk.
//
// It is a projection rather than entries carried in the definition, which is the distinction
// that matters: a chart is not uploaded to a host, so a real file entry would have it delivered
// to every controller for nothing. Target is set because it is how a staged path is named and
// for no other reason -- nothing writes a chart to that path.
func ChartFiles(charts []Chart) v1alpha1.Files {
	files := make(v1alpha1.Files, 0, len(charts))
	for _, chart := range charts {
		files = append(files, &v1alpha1.File{
			Name:   chart.Name,
			Source: chart.Source,
			Shasum: chart.Shasum,
			Target: "/" + chart.TarballName(),
		})
	}
	return files
}
