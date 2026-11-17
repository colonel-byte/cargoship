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

// This file holds the charts the chart-carrying flavors deploy, and what pins each of them. The
// flavors themselves are declared in distros.go, with the rest.

package examples

import (
	"context"
	"fmt"

	"github.com/colonel-byte/cargoship/pkg/coci"
)

// exampleChart is what a chart flavor's templates render from. Nil on every other flavor, so
// one `{{ with .Chart }}` in each distro template covers the whole feature.
type exampleChart struct {
	// Kind is where the chart came from, which the values templates branch on: what a
	// cluster can configure is the chart's own vocabulary, so it differs per chart.
	Kind chartSource
	// Name is the HelmChart resource's name.
	Name string
	// Source is where the build reads the chart from: a URL, or an oci:// reference pinned to
	// a digest.
	Source string
	// Version names the tarball for a source that does not, which is every oci:// one.
	Version string
	// Shasum is the tarball's SHA-256, which is what the build verifies the bytes against.
	// Empty for a source pinned by digest, which is its own guarantee.
	Shasum string
	// Namespace is the namespace the release is installed into.
	Namespace string
	// Images are the images the chart's workloads run, carried in the package so the chart
	// has something to pull on a host that can reach no registry.
	Images []string
	// Values is the chart's values block, as the YAML the definition carries. Empty for a
	// chart installed with its own defaults.
	Values string
}

// resolveExampleChart is the chart block for one flavor, by where its chart comes from.
//
// No flavor carries a chart tarball committed in this repository, and that is deliberate: a
// `.tgz` in a source tree is bytes nobody reviews, in a file format a reviewer cannot diff, and
// the xz-utils backdoor was exactly that. `.spec.config.charts` accepts a local path -- it is
// the first thing documented in docs/guides/charts.md -- but an example that shipped one would
// be asking every reader to trust an opaque blob to demonstrate a feature two fetched charts
// demonstrate just as well.
func resolveExampleChart(src chartSource, sums *exampleShasums) (*exampleChart, error) {
	switch src {
	case chartSourceHTTP:
		return newExampleHTTPChart(sums)
	case chartSourceOCI:
		return newExampleOCIChart(sums)
	default:
		return nil, fmt.Errorf("unknown chart source %q", src)
	}
}

// The two charts the remote-source flavors carry, and what pins each of them.
//
// They exist to exercise the two ways a chart arrives from somewhere other than the definition
// directory, because those are the paths an operator actually uses -- a chart beside the
// definition is the demonstration, and a chart from a repository or a registry is the real
// thing. Each is pinned the way its own source allows:
//
//   - the http one by the digest its chart repository publishes in index.yaml, so the pin costs
//     one small request and never downloads the chart to arrive at a number upstream already
//     states;
//   - the oci one by the manifest digest its tag resolved to, which needs no shasum at all:
//     the reference is the guarantee, and cargoship refuses an oci:// tag that carries neither.
//
// Both are hand-edited pins, like the CNI versions in upstream_cni.go. Moving one means editing
// the version here and re-running mage generate:examples, which re-reads the index or re-resolves
// the tag.
const (
	// exampleHTTPChart is metrics-server, downloaded from the release its chart repository
	// serves the tarball out of. Small, stable, and something a cluster plausibly wants,
	// which is what makes it worth carrying in an example rather than inventing a chart.
	exampleHTTPChartName    = "metrics-server"
	exampleHTTPChartVersion = "3.14.0"

	// exampleOCIChart is prometheus-node-exporter, published as an OCI artifact the way
	// `helm push` publishes one. A Helm chart artifact is a single-layer manifest, so it
	// needs no layer selector -- see pkg/coci.FileSelector.
	exampleOCIChartName    = "prometheus-node-exporter"
	exampleOCIChartVersion = "4.13.1"
	exampleOCIChartRepo    = "oci://ghcr.io/prometheus-community/charts/prometheus-node-exporter"
)

// exampleHTTPChartURL is where the pinned version of the chart is published. The path is the
// project's own release asset layout, built here rather than read out of the repository's
// index.yaml -- fetchLines trims every line it reads, which is right for a checksum list and
// destroys a YAML document's indentation, and a 14KiB chart is not worth a second downloader to
// avoid hashing.
func exampleHTTPChartURL() string {
	return fmt.Sprintf("https://github.com/kubernetes-sigs/%s/releases/download/%s-helm-chart-%s/%s-%s.tgz",
		exampleHTTPChartName, exampleHTTPChartName, exampleHTTPChartVersion, exampleHTTPChartName, exampleHTTPChartVersion)
}

// newExampleHTTPChart hashes the published chart, through the same cache every other remote
// file the examples carry goes through -- so it is downloaded once per pin and read from
// example/shasums.json afterwards.
func newExampleHTTPChart(sums *exampleShasums) (*exampleChart, error) {
	url := exampleHTTPChartURL()
	digest, err := sums.get(url)
	if err != nil {
		return nil, err
	}
	// get answers a URL upstream no longer serves with an empty digest, because one missing
	// RPM should not fail a whole render. A chart is different: without a digest the
	// definition is refused when it is built, so the pin is what has gone and saying so here
	// is the only place that reads as the actual problem.
	if digest == "" {
		return nil, fmt.Errorf("%s is no longer published: move the pinned %s chart version", url, exampleHTTPChartName)
	}
	return &exampleChart{
		Kind:      chartSourceHTTP,
		Name:      exampleHTTPChartName,
		Source:    url,
		Version:   exampleHTTPChartVersion,
		Shasum:    digest,
		Namespace: "kube-system",
		Images:    []string{exampleHTTPChartImage},
		// metrics-server cannot verify a kubelet's serving certificate on a cluster that
		// has not had one signed, which is most of them, so whether it skips that check is
		// the knob this flavor exposes.
		Values: "{{- if .Values.chart.insecureKubeletTLS }}\nargs:\n  - --kubelet-insecure-tls\n{{- end }}",
	}, nil
}

// exampleHTTPChartImage is the image metrics-server's chart runs. It is pinned beside the chart
// version because the chart's appVersion decides it, and a package that carried the wrong one
// would install a chart whose pod cannot pull.
const exampleHTTPChartImage = "registry.k8s.io/metrics-server/metrics-server:v0.8.0"

// newExampleOCIChart pins the chart's reference to the digest its tag resolves to, the way
// resolveK3sImage pins the k3s image: a tag can be re-pushed, a digest cannot, and a reference
// that names a digest needs no shasum beside it.
func newExampleOCIChart(sums *exampleShasums) (*exampleChart, error) {
	ref := fmt.Sprintf("%s:%s", exampleOCIChartRepo, exampleOCIChartVersion)
	pinned, err := pinChartReference(ref, sums)
	if err != nil {
		return nil, err
	}
	return &exampleChart{
		Kind:      chartSourceOCI,
		Name:      exampleOCIChartName,
		Source:    pinned,
		Version:   exampleOCIChartVersion,
		Namespace: "monitoring",
		Images:    []string{exampleOCIChartImage},
		// The exporter runs on every node including the controllers, which carry a taint a
		// DaemonSet has to tolerate, and whether it should is a cluster's decision.
		Values: "{{- if .Values.chart.tolerateControlPlane }}\ntolerations:\n  - operator: Exists\n{{- end }}",
	}, nil
}

// exampleOCIChartImage is the image prometheus-node-exporter's chart runs; see
// exampleHTTPChartImage.
const exampleOCIChartImage = "quay.io/prometheus/node-exporter:v1.10.0"

// pinChartReference resolves an oci:// chart reference to the digest its tag names, so the
// rendered example addresses bytes rather than a tag that can be re-pushed.
//
// Only manifests are read -- a Helm chart artifact is a single-layer manifest, so there is no
// index to walk and no layer to fetch -- which is why pinning one costs about what reading a
// release checksum file does. The resolution is cached under the reference, like the k3s image's.
func pinChartReference(ref string, sums *exampleShasums) (string, error) {
	if e, ok := sums.lookupKey(ref, ref); ok {
		return e.SHA256, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), exampleOCITimeout)
	defer cancel()

	resolution, err := coci.Resolve(ctx, ref, coci.FileSelector{}, coci.FileOptions{})
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", ref, err)
	}
	sums.storeKey(ref, ref, resolution.Reference)
	return resolution.Reference, nil
}
