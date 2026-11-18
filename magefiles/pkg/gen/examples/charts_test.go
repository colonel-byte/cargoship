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
	"text/template"

	"github.com/colonel-byte/cargoship/internal/cfg"
	"github.com/colonel-byte/cargoship/pkg/helmvalues"
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
	for _, dir := range []string{"k3s-chart-http", "k3s-chart-oci", "k3s-service-lb"} {
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

			require.NotEmpty(t, dis.Spec.Config.Charts)
			for _, chart := range dis.Spec.Config.Charts {
				require.Equal(t, ".chart.enabled", chart.When)
				require.NotEmpty(t, chart.TarballName())
				// Every chart these flavors carry is fetched, so each one has to name a
				// digest or be pinned by one -- which is what the decoder refuses on.
				require.True(t, chart.Shasum != "" || strings.Contains(chart.Source, "@sha256:"),
					"chart %s is fetched with neither a shasum nor a digest in its reference", chart.Name)
			}
			// Not file entries: a chart reaches the cluster inside its resource, so a
			// package that uploaded one to every controller would be carrying it twice.
			for _, f := range dis.Spec.Config.Files {
				for _, chart := range dis.Spec.Config.Charts {
					require.NotContains(t, f.Target, chart.TarballName())
				}
			}
		})
	}
}

// The Service load balancer flavor is the only one carrying more than one chart, and the pair
// is load-bearing: kube-vip answers ARP for the addresses, the cloud provider decides them, and
// either on its own leaves every Service Pending. The digests come out of a pre-seeded cache, so
// this reaches no network.
func TestNewExampleServiceLBChartsCarriesThePair(t *testing.T) {
	sums := &exampleShasums{sums: map[string]exampleShasum{}}
	for _, nv := range [][2]string{
		{"kube-vip", exampleSvcLBChartVersion},
		{"kube-vip-cloud-provider", exampleSvcLBCloudVersion},
	} {
		url := exampleSvcLBChartURL(nv[0], nv[1])
		sums.storeKey(archiveKey(url), url, "aaaa"+nv[0])
	}

	charts, err := newExampleServiceLBCharts(sums)
	require.NoError(t, err)
	require.Len(t, charts, 2)

	names := map[string]bool{}
	sources := map[string]bool{}
	for _, c := range charts {
		require.NotEmpty(t, c.Shasum, "chart %s is fetched and carries no shasum", c.Name)
		require.NotEmpty(t, c.Images, "chart %s carries no image, so an air-gapped cluster cannot run it", c.Name)
		require.False(t, names[c.Name], "two charts named %s would write one HelmChart resource", c.Name)
		require.False(t, sources[c.Source], "two charts fetched from %s are one chart declared twice", c.Source)
		names[c.Name] = true
		sources[c.Source] = true
	}

	// The cloud provider's chart defaults image.tag to its own appVersion, so leaving it unset
	// renders a Deployment pointing at whatever the chart was cut against rather than at the
	// image this package carries -- which is an ImagePullBackOff on an air-gapped cluster and
	// nothing at all at build time.
	for _, c := range charts {
		require.Contains(t, c.Values, "image:\n  tag: ", "chart %s does not pin the image tag it carries", c.Name)
	}
}

// Two charts' values are indented into one distro template, which is what made trimSuffix
// necessary: a values block ending in a newline rendered a line of nothing but spaces, and
// trailing whitespace is the one thing the hooks rewrite rather than report.
func TestServiceLBChartValuesRenderWithoutTrailingWhitespace(t *testing.T) {
	sums := &exampleShasums{sums: map[string]exampleShasum{}}
	for _, nv := range [][2]string{
		{"kube-vip", exampleSvcLBChartVersion},
		{"kube-vip-cloud-provider", exampleSvcLBCloudVersion},
	} {
		url := exampleSvcLBChartURL(nv[0], nv[1])
		sums.storeKey(archiveKey(url), url, "aaaa")
	}
	charts, err := newExampleServiceLBCharts(sums)
	require.NoError(t, err)

	// The repository's own funcmap, not a stand-in: sprig's trimSuffix takes the suffix
	// first, and a hand-rolled one with the arguments the other way round renders a values
	// block that looks fine and is not the one the generator produces.
	tmpl, err := template.New("distro").Funcs(helmvalues.FuncMap()).
		Parse("charts:\n{{- range .Charts }}\n  - name: {{ .Name }}\n    values: |\n{{ trimSuffix \"\\n\" .Values | indent 6 }}\n{{- end }}\n")
	require.NoError(t, err)

	var out strings.Builder
	require.NoError(t, tmpl.Execute(&out, map[string]any{"Charts": charts}))

	for i, line := range strings.Split(out.String(), "\n") {
		require.Equal(t, strings.TrimRight(line, " \t"), line, "line %d ends in whitespace: %q", i+1, line)
	}
	require.Contains(t, out.String(), "- name: kube-vip\n")
	require.Contains(t, out.String(), "- name: kube-vip-cloud-provider\n")
}
