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

// This file holds the CNI each upstream example bakes in: the three flavors
// example/upstream-<cni> are rendered for, where each one's manifest comes from, and the images
// that manifest runs.
//
// Upstream ships no CNI of its own, so a package declaring none installs a cluster whose pods
// never leave ContainerCreating. spec.config.manifests -- kubectl-applied from the leader by
// pkg/phase/64_apply_manifests.go once it has bootstrapped -- is what closes that, and this is
// what fills it in. Rancher's distros need none of this: rke2 and k3s ship their CNIs with the
// release, so their flavors only have to name one.

package examples

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// The CNI versions the upstream examples pin. A CNI release supports a range of
	// Kubernetes minor lines rather than one of them, so a single pin per CNI covers every
	// line the examples render, and moving one is a hand edit here.
	//
	// Each manifest URL names its version rather than using an alias like
	// "releases/latest/download". An alias serves different bytes from one stable URL, which
	// is the one thing example/shasums.json cannot represent: it keys an entry by file name,
	// so the digest recorded for kube-flannel.yml would flip on every upstream release.
	upstreamFlannelVersion = "v0.28.10"
	upstreamCanalVersion   = "v3.33.0"

	// upstreamCiliumChartVersion is a Helm chart version, not a manifest version: cilium
	// publishes a chart and nothing else -- install/kubernetes/quick-install.yaml has 404'd
	// since v1.10 -- so its manifest is rendered here rather than downloaded. See
	// ensureCiliumManifest.
	upstreamCiliumChartVersion = "1.20.2"

	// upstreamCiliumChartRepo is the chart repository that render reads from.
	upstreamCiliumChartRepo = "https://helm.cilium.io"

	// upstreamCNITargetDir is where a flavor's manifest lands on a controller. It is below
	// /etc/kubernetes, which Upstream.CleanupPaths() already removes on uninstall, and is
	// deliberately not /etc/kubernetes/manifests: that directory is the kubelet's static pod
	// directory, and a CNI DaemonSet dropped in it would be read as a pod to run directly.
	upstreamCNITargetDir = "/etc/kubernetes/manifests-cni"

	// upstreamCNIDirName is the directory inside a flavor holding a manifest rendered here
	// rather than downloaded. It sits beside the minor line directories, so every version of
	// the flavor shares the one file instead of committing a copy per release -- the same
	// arrangement example/k3s/core has for the files every k3s version installs.
	upstreamCNIDirName = "cni"
)

// upstreamCNIFlavor is one CNI the upstream examples are rendered for, and the directory its
// examples are written to. The CNI reaches further than the image list, which is why each one
// gets a flavor rather than a value in a shared example: it decides the manifest that gets
// applied and the pod CIDR that manifest expects kubeadm to have been told about.
type upstreamCNIFlavor struct {
	cni string // flannel -- what follows "upstream-" in metadata.name
	dir string // example/upstream-flannel -- where its examples are written

	// manifest is the pinned URL the package downloads its manifest from. Empty for a CNI
	// publishing no applyable manifest, which is rendered from chart instead.
	manifest string
	// chart is the pinned Helm chart version rendered into the flavor's own cni/ directory.
	// Empty for a CNI that publishes a manifest.
	chart string

	// podSubnet is the pod CIDR the manifest hardcodes, written into the engine config so
	// kubeadm hands the same one to the controller manager. Empty for a CNI that reads the
	// cluster's own CIDR rather than carrying one.
	podSubnet string
}

// upstreamCNIFlavors is every flavor the upstream examples are rendered for.
//
// flannel and canal both bake 10.244.0.0/16 into the manifest they publish -- flannel in its
// net-conf.json, canal in the flannel ConfigMap it bundles -- so both pin kubeadm to the same
// CIDR. Disagreeing on that is not an error either side reports: the cluster comes up and pod
// traffic silently does not route. Cilium reads the CIDR out of the cluster, so it sets none,
// and runs alongside kube-proxy rather than replacing it, since kubeadm's skipPhases is not
// something cargoship renders yet.
var upstreamCNIFlavors = []upstreamCNIFlavor{
	{
		cni:   "cilium",
		dir:   "example/upstream-cilium",
		chart: upstreamCiliumChartVersion,
	},
	{
		cni:       "canal",
		dir:       "example/upstream-canal",
		manifest:  "https://raw.githubusercontent.com/projectcalico/calico/" + upstreamCanalVersion + "/manifests/canal.yaml",
		podSubnet: "10.244.0.0/16",
	},
	{
		cni:       "flannel",
		dir:       "example/upstream-flannel",
		manifest:  "https://github.com/flannel-io/flannel/releases/download/" + upstreamFlannelVersion + "/kube-flannel.yml",
		podSubnet: "10.244.0.0/16",
	},
}

// upstreamCNI is one flavor's manifest resolved for a render run: where the package reads it
// from, the digest it is verified against, the path it is applied from, and the images it runs.
// None of that varies by Kubernetes version, so it is resolved once per flavor rather than once
// per example -- fifty-nine versions of a flavor would otherwise re-ask for the same answer.
type upstreamCNI struct {
	Name      string   // flannel
	Source    string   // the URL, or a path relative to the example directory
	Target    string   // /etc/kubernetes/manifests-cni/kube-flannel.yml
	SHA256    string   // empty for a manifest rendered into the repository, which has no remote bytes to verify
	Images    []string // the images the manifest runs, in the order it names them
	PodSubnet string
}

// manifestFile is the file name the flavor's manifest is known by, on a host and in the
// repository. A rendered manifest carries its chart version, since the file is shared by every
// version of the flavor and a chart bump has to read as a different file.
func (f upstreamCNIFlavor) manifestFile() string {
	if f.manifest != "" {
		return path.Base(f.manifest)
	}
	return fmt.Sprintf("%s-%s.yaml", f.cni, f.chart)
}

// localManifestPath is where a rendered manifest is written in the repository.
func (f upstreamCNIFlavor) localManifestPath() string {
	return filepath.Join(f.dir, upstreamCNIDirName, f.manifestFile())
}

// localManifestSource is how an example refers to a rendered manifest: a path relative to the
// directory holding its distro.yaml, which is <flavor>/<minor>/<version>, so two levels up from
// the example reaches the flavor root the cni/ directory sits in. Depth is hand-encoded here
// and nothing but a render checks it, so it moves with the example layout, not on its own.
func (f upstreamCNIFlavor) localManifestSource() string {
	return path.Join("..", "..", upstreamCNIDirName, f.manifestFile())
}

// resolve answers what the flavor's examples need to know about its CNI: a downloaded manifest
// is hashed and read for its images, a chart is rendered first and then read the same way.
func (f upstreamCNIFlavor) resolve(sums *exampleShasums) (upstreamCNI, error) {
	cni := upstreamCNI{
		Name:      f.cni,
		Target:    path.Join(upstreamCNITargetDir, f.manifestFile()),
		PodSubnet: f.podSubnet,
	}

	if f.manifest == "" {
		local := f.localManifestPath()
		if err := ensureCiliumManifest(local, f.chart); err != nil {
			return upstreamCNI{}, err
		}
		body, err := os.ReadFile(local)
		if err != nil {
			return upstreamCNI{}, err
		}
		cni.Source = f.localManifestSource()
		cni.Images = manifestImages(splitReleaseLines(body))
		return f.checked(cni)
	}

	// Unlike an rke2 RPM, which Rancher prunes once the next revision supersedes it, a
	// pinned CNI release stays published -- so a missing digest here is a broken pin to fix
	// rather than a retirement to render around.
	sum, err := sums.get(f.manifest)
	if err != nil {
		return upstreamCNI{}, err
	}
	if sum == "" {
		return upstreamCNI{}, fmt.Errorf("%s no longer serves %s: update the pinned %s version", f.manifest, f.manifestFile(), f.cni)
	}
	lines, err := fetchLines(f.manifest)
	if err != nil {
		return upstreamCNI{}, err
	}

	cni.Source = f.manifest
	cni.SHA256 = sum
	cni.Images = manifestImages(lines)
	return f.checked(cni)
}

// checked refuses a manifest no image could be read out of. That means the scrape stopped
// matching what the CNI publishes, and the examples it would write install a CNI whose images
// are not in the package -- which only shows up on an air-gapped host, at apply time.
func (f upstreamCNIFlavor) checked(cni upstreamCNI) (upstreamCNI, error) {
	if len(cni.Images) == 0 {
		return upstreamCNI{}, fmt.Errorf("no images found in %s for %s", f.manifestFile(), f.cni)
	}
	return cni, nil
}

// resolveUpstreamCNIs resolves every flavor's manifest up front, so a run that cannot answer
// for one of them fails before it writes examples for the others.
func resolveUpstreamCNIs(sums *exampleShasums) (map[string]upstreamCNI, error) {
	out := make(map[string]upstreamCNI, len(upstreamCNIFlavors))
	for _, f := range upstreamCNIFlavors {
		cni, err := f.resolve(sums)
		if err != nil {
			return nil, err
		}
		out[f.cni] = cni
	}
	return out, nil
}

// manifestImagePattern matches a manifest's container image lines. The lines it is handed are
// already trimmed, so the anchor is what keeps imagePullPolicy and a commented-out image out of
// the list; the quotes are what helm renders around a value.
var manifestImagePattern = regexp.MustCompile(`^image:\s*"?([^"\s]+)"?$`)

// manifestImages is every image a manifest runs, in the order it names them and without
// duplicates -- a DaemonSet naming the same image in an init container and a container is the
// usual case, and imageConfig.images has to be unique.
//
// Reading the images out of the manifest is the point: a package's image list is then what the
// CNI it applies actually pulls, rather than a second list maintained here that agrees with it
// until the CNI adds a sidecar.
func manifestImages(lines []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range lines {
		m := manifestImagePattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		image := m[1]
		// A chart rendered with a value left unset leaves the placeholder behind rather
		// than an image, and there is nothing to pull for it.
		if strings.ContainsAny(image, "{}") || seen[image] {
			continue
		}
		seen[image] = true
		out = append(out, image)
	}
	return out
}

// ciliumRenderValues is the values document the cilium render overrides the chart's defaults
// with. Two overrides, each forced by something the default does that a committed manifest
// cannot carry.
//
// hubble.tls.auto.method: the default ("helm") generates Hubble's CA keypair during templating
// and writes it into a Secret in the output -- the chart labels that object
// cilium.io/helm-template-non-idempotent itself. The render would stop being reproducible, and
// the private key would be committed here, handing every cluster installed from this example the
// same Hubble CA. "cronJob" has cilium's own certgen job mint the certificates in the cluster
// instead. The certgen image that adds is picked up by the image scrape like any other.
//
// useDigest: the chart pins each image by digest, and for a multi-architecture image that digest
// is the index's, not a platform's. Zarf refuses an index -- "resolved to an OCI image index
// which is not supported" -- so a package built from the rendered manifest fails at create time,
// which is where this was found. Turning it off leaves the tag, which resolves per platform.
// What is given up is the digest pinning, and the package re-pins everything by digest when it
// builds, so the image set a built package carries is still exact.
//
// The useDigest keys are read out of the chart's own values rather than listed here: there are
// sixteen in 1.20.2, across images most clusters never enable, and a chart that adds a
// seventeenth would otherwise reintroduce the failure on the next pin bump.
func ciliumRenderValues(helm, chart string) (map[string]any, error) {
	defaults, err := ciliumChartValues(helm, chart)
	if err != nil {
		return nil, err
	}

	values := map[string]any{
		"hubble": map[string]any{
			"tls": map[string]any{
				"auto": map[string]any{"method": "cronJob"},
			},
		},
	}
	paths := useDigestPaths(defaults, nil)
	if len(paths) == 0 {
		return nil, fmt.Errorf("cilium chart %s declares no useDigest values: the chart's image pinning has changed, check whether it still pins an image index", chart)
	}
	for _, path := range paths {
		setValue(values, path, false)
	}
	return values, nil
}

// ciliumChartValues reads the chart's default values.
func ciliumChartValues(helm, chart string) (map[string]any, error) {
	cmd := exec.Command(helm, "show", "values", "cilium",
		"--repo", upstreamCiliumChartRepo,
		"--version", chart,
	)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("reading cilium chart %s values: %w: %s", chart, err, strings.TrimSpace(stderr.String()))
	}

	var values map[string]any
	if err := yaml.Unmarshal(out.Bytes(), &values); err != nil {
		return nil, fmt.Errorf("parsing cilium chart %s values: %w", chart, err)
	}
	return values, nil
}

// useDigestPaths walks a values tree and returns the dotted path of every useDigest set to true.
func useDigestPaths(node map[string]any, prefix []string) [][]string {
	var out [][]string
	for key, value := range node {
		path := append(append([]string{}, prefix...), key)
		if key == "useDigest" {
			if enabled, ok := value.(bool); ok && enabled {
				out = append(out, path)
			}
			continue
		}
		if child, ok := value.(map[string]any); ok {
			out = append(out, useDigestPaths(child, path)...)
		}
	}
	// Map iteration is unordered, and these paths become a values document the render reads.
	// Sorting them keeps that document, and so the render, byte-identical run to run.
	slices.SortFunc(out, func(a, b []string) int { return strings.Compare(strings.Join(a, "."), strings.Join(b, ".")) })
	return out
}

// setValue writes value at a nested path, creating the maps it passes through.
func setValue(root map[string]any, path []string, value any) {
	node := root
	for _, key := range path[:len(path)-1] {
		child, ok := node[key].(map[string]any)
		if !ok {
			child = map[string]any{}
			node[key] = child
		}
		node = child
	}
	node[path[len(path)-1]] = value
}

// writeTempValues writes a values document for helm to read, and returns its path. The overrides
// go in a file rather than in --set flags because --set takes a dotted path as its key, and
// several of the chart's own keys contain dots.
func writeTempValues(values map[string]any) (string, error) {
	data, err := yaml.Marshal(values)
	if err != nil {
		return "", err
	}

	f, err := os.CreateTemp("", "cilium-values-*.yaml")
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck // the write below is what has to succeed

	if _, err := f.Write(data); err != nil {
		os.Remove(f.Name()) //nolint:errcheck // already failing, and a temp file is the lesser problem
		return "", err
	}
	return f.Name(), nil
}

// ensureCiliumManifest renders the pinned cilium chart to path, and does nothing when that file
// is already in the repository.
//
// Skipping the render is what keeps helm out of the loop that keeps the examples current: the
// rendered manifest is committed, and only a chart pin bump produces a path that does not exist
// yet. So refresh-examples.yaml -- which renders every example weekly and never touches this
// pin -- needs nothing but the Go toolchain, and the helm dependency falls on whoever moves the
// pin, locally, where fixing it is a package install.
func ensureCiliumManifest(path, chart string) error {
	switch _, err := os.Stat(path); {
	case err == nil:
		return nil
	case !os.IsNotExist(err):
		return err
	}

	helm, err := exec.LookPath("helm")
	if err != nil {
		return fmt.Errorf("rendering cilium chart %s needs helm on PATH: %w", chart, err)
	}

	values, err := ciliumRenderValues(helm, chart)
	if err != nil {
		return err
	}
	valuesFile, err := writeTempValues(values)
	if err != nil {
		return err
	}
	defer os.Remove(valuesFile) //nolint:errcheck // a leftover temp file is not worth failing a render over

	// --namespace matches where the cluster's own control plane lives, since the rendered
	// manifest names the namespace in every object it carries and nothing re-reads a
	// kubectl-applied file's placement afterward.
	cmd := exec.Command(helm, "template", "cilium", "cilium",
		"--repo", upstreamCiliumChartRepo,
		"--version", chart,
		"--namespace", "kube-system",
		"--values", valuesFile,
	)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rendering cilium chart %s: %w: %s", chart, err, strings.TrimSpace(stderr.String()))
	}
	if out.Len() == 0 {
		return fmt.Errorf("rendering cilium chart %s produced nothing", chart)
	}
	if err := refuseEmbeddedKey(out.Bytes(), chart); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := writeFileAtomic(path, trimTrailingSpace(out.Bytes())); err != nil {
		return err
	}
	fmt.Println("Rendered " + path)

	// A pin bump leaves the previous chart's manifest behind, referred to by nothing, so it
	// goes with the render that replaces it rather than waiting for someone to notice.
	return pruneStaleManifests(filepath.Dir(path), filepath.Base(path))
}

// keyMarkers are what a private key looks like in a rendered manifest: the PEM header, which
// every key type shares the shape of, and the base64 each header's first eighteen characters
// encode to, since a Secret holds the whole key base64-encoded from its first byte.
//
// Eighteen is not arbitrary. base64 encodes three bytes at a time, so a prefix whose length is a
// multiple of three encodes to the same characters whatever follows it; a longer prefix would
// only match a key whose header happened to align the same way.
var keyMarkers = []string{
	"PRIVATE KEY-----",
	"LS0tLS1CRUdJTiBSU0EgUFJJ", // -----BEGIN RSA PRI
	"LS0tLS1CRUdJTiBQUklWQVRF", // -----BEGIN PRIVATE
	"LS0tLS1CRUdJTiBFQyBQUklW", // -----BEGIN EC PRIV
}

// refuseEmbeddedKey fails a render whose output carries a private key.
//
// A chart that mints a keypair while templating produces a manifest that cannot be committed:
// the key would be in the repository, and every cluster installed from the example would share
// one CA. The render is configured not to do that (see the hubble.tls.auto.method note above),
// so this is the check that the configuration still holds -- a chart's defaults can change under
// a version bump, and the failure mode is silent by nature.
func refuseEmbeddedKey(rendered []byte, chart string) error {
	body := string(rendered)
	for _, marker := range keyMarkers {
		if strings.Contains(body, marker) {
			return fmt.Errorf("cilium chart %s rendered a private key into its manifest: it cannot be committed, and every cluster installed from it would share one CA. Check what the chart now generates at template time", chart)
		}
	}
	return nil
}

// trimTrailingSpace drops trailing whitespace from every line and leaves exactly one newline at
// the end, which is what the trailing-whitespace and end-of-file-fixer pre-commit hooks would
// do to the file anyway. Doing it here instead means the bytes the generator writes are the
// bytes that get committed, so a chart bump is not followed by a hook rewriting the render.
//
// Only whitespace-only padding is touched. Helm leaves it at the end of lines and inside the
// block scalars the chart's shell snippets live in, where a line of nothing but indentation is
// an empty line either way.
func trimTrailingSpace(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return []byte(strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n")
}

// pruneStaleManifests removes every rendered manifest in dir but keep.
func pruneStaleManifests(dir, keep string) error {
	entries, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if filepath.Base(entry) == keep {
			continue
		}
		if err := os.Remove(entry); err != nil {
			return err
		}
		fmt.Println("Removed " + entry)
	}
	return nil
}
