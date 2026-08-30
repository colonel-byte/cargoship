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

// This file holds the pieces both example-rendering targets share: what an example is
// rendered from, and how one is written. The targets themselves each live in their own
// gen-example*.go file.

package main

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"text/template"
)

const (
	// The selinux policy RPMs are shared by every version of their distro, unlike the RPMs
	// and binaries that carry the build's own version.
	exampleRKE2SelinuxRPM = "https://rpm.rancher.io/rke2/latest/common/centos/9/noarch/rke2-selinux-0.22-1.el9.noarch.rpm"
	exampleK3sSelinuxRPM  = "https://rpm.rancher.io/k3s/latest/common/centos/9/noarch/k3s-selinux-1.6-1.el9.noarch.rpm"
)

// exampleFlavor is one build of a distro the template renders. Flavors are named for their
// CNI and written to their own directory, since the CNI choice reaches further than the
// image list: cilium replaces kube-proxy and is configured through a HelmChartConfig
// manifest, while canal and flannel run alongside kube-proxy and need no manifest.
type exampleFlavor struct {
	cni               string // cilium -- the flavor's name, and what the template configures
	dir               string // example/rke2-cilium -- where its examples are written
	imageList         string // rke2-images-cilium.linux-amd64.txt -- its CNI's airgap manifest, when it has one of its own
	replacesKubeProxy bool   // whether the CNI takes over from kube-proxy
}

// exampleDistroSpec is everything that differs between the distros examples are rendered
// for. The distros agree on more than they differ on -- a tag, a template, image manifests
// published with the release -- so what varies is pushed into the three hooks rather than
// into two copies of the rendering itself.
type exampleDistroSpec struct {
	name     string // the pins.json entry, and the distro name embedded in a tag
	template string
	flavors  []exampleFlavor

	// coreImages is the release's own image manifest: everything the distro ships before a
	// flavor adds its CNI's images. RKE2 splits the two, k3s publishes one combined list.
	// Either way an example's images are what that release ships, not a list rebuilt here.
	coreImages string

	// derive fills in the fields only this distro has, from the version already parsed.
	// Offline, so probe has something to check before anything is fetched.
	derive func(v *exampleVersion)

	// probe returns the URL whose absence means this build can no longer be installed, and
	// so should not be kept as an example. Empty means the distro publishes nothing that
	// disappears out from under a live git tag.
	probe func(v exampleVersion) string

	// fetch pulls anything else the template needs that only the release can answer. May be
	// nil.
	fetch func(v *exampleVersion, repoURL string) error
}

// exampleDistros is every distro the example targets render.
var exampleDistros = []exampleDistroSpec{
	{
		name:       "rke2",
		template:   "magefiles/templates/rke2-distro.yaml.tmpl",
		coreImages: "rke2-images-core.linux-amd64.txt",
		flavors: []exampleFlavor{
			{
				cni:               "cilium",
				dir:               "example/rke2-cilium",
				imageList:         "rke2-images-cilium.linux-amd64.txt",
				replacesKubeProxy: true,
			},
			{
				cni:       "canal",
				dir:       "example/rke2-canal",
				imageList: "rke2-images-canal.linux-amd64.txt",
			},
		},
		derive: func(v *exampleVersion) {
			v.SelinuxRPM = exampleRKE2SelinuxRPM
			v.CommonRPM = exampleRKE2RPM("common", v.Minor, v.RPMVersion)
			v.ServerRPM = exampleRKE2RPM("server", v.Minor, v.RPMVersion)
			v.AgentRPM = exampleRKE2RPM("agent", v.Minor, v.RPMVersion)
		},
		// RKE2 installs from RPMs, and Rancher removes an rke2rN's RPMs once the next
		// revision supersedes it, long before the git tag goes anywhere.
		probe: func(v exampleVersion) string { return v.CommonRPM },
	},
	{
		name:       "k3s",
		template:   "magefiles/templates/k3s-distro.yaml.tmpl",
		coreImages: "k3s-images.txt",
		// k3s ships its CNI in the binary, so flannel is what a stock k3s runs, and its
		// images are already in k3s-images.txt.
		flavors: []exampleFlavor{
			{cni: "flannel", dir: "example/k3s-flannel"},
		},
		derive: func(v *exampleVersion) {
			v.SelinuxRPM = exampleK3sSelinuxRPM
			v.BinaryURL = fmt.Sprintf("https://github.com/k3s-io/k3s/releases/download/%s/k3s", v.TagURL)
		},
		fetch: fetchK3sBinarySHA,
	},
}

// exampleMinorPattern matches the minor line directories examples are grouped under
// (example/rke2-cilium/v1_35), so anything else at that level is left alone. The naming
// matches thirdparty-src/<distro>/ and src/pkg/engineconfig/gen/<distro>/.
var exampleMinorPattern = regexp.MustCompile(`^v[0-9]+_[0-9]+$`)

// exampleVersion is what the templates render against. Every field is derived from one tag
// and the flavor being rendered, so a whole example follows from pins.json plus the flavor.
type exampleVersion struct {
	Version           string   // 1.36.4-rke2r1 -- metadata.version and spec.version
	Minor             string   // 1.36 -- the channel in the rpm.rancher.io paths
	RPMVersion        string   // 1.36.4~rke2r1 -- RPM file names
	TagURL            string   // v1.36.4%2Brke2r1 -- the release download path segment
	CNI               string   // cilium
	ReplacesKubeProxy bool     // whether to set disable-kube-proxy
	CoreImages        []string // the release's own image manifest
	CNIImages         []string // the flavor's CNI manifest, when it has one

	// The remote files the example installs, and the selinux policy RPM both distros share.
	// They are built here rather than in the templates so that the URL checked against
	// upstream is the same one the example carries.
	SelinuxRPM string

	// RKE2 installs from RPMs.
	CommonRPM string
	ServerRPM string
	AgentRPM  string

	// k3s installs a single binary, whose digest the release publishes for us.
	BinaryURL string
	BinarySHA string
}

// exampleRKE2RPM is the rpm.rancher.io URL of one of a build's versioned RPMs.
func exampleRKE2RPM(pkg, minor, rpmVersion string) string {
	return fmt.Sprintf("https://rpm.rancher.io/rke2/latest/%s/centos/9/x86_64/rke2-%s-%s-0.el9.x86_64.rpm",
		minor, pkg, rpmVersion)
}

// fetchK3sBinarySHA reads the k3s binary's digest out of the release's own checksum file.
// Every k3s release publishes one, so the binary itself -- tens of megabytes, and named
// plainly enough that every version would collide in the cache -- never has to be
// downloaded to be verified.
func fetchK3sBinarySHA(v *exampleVersion, repoURL string) error {
	const asset = "sha256sum-amd64.txt"

	lines, err := fetchReleaseLines(repoURL, v.TagURL, asset)
	if err != nil {
		return err
	}
	for _, line := range lines {
		// "<sha256>  k3s", alongside the airgap tarballs.
		if sum, name, ok := strings.Cut(line, " "); ok && strings.TrimSpace(name) == "k3s" {
			v.BinarySHA = sum
			return nil
		}
	}
	return fmt.Errorf("no k3s entry in %s for %s", asset, v.TagURL)
}

// parseExampleTemplate parses a distro's example template, wiring in the sha256 function its
// remote file entries are hashed with.
func parseExampleTemplate(spec exampleDistroSpec, sums *exampleShasums) (*template.Template, error) {
	tmpl, err := template.New(filepath.Base(spec.template)).
		Funcs(template.FuncMap{"sha256": sums.get}).
		ParseFiles(spec.template)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", spec.template, err)
	}
	return tmpl, nil
}

// exampleTags is every tag one flavor renders an example for: the pinned ones plus whatever
// that flavor already has on disk, newest first. Examples live at
// <flavor dir>/<minor>/<tag>/distro.yaml, and a tag directory is named for its tag with the
// "+" swapped for a "-", since "+" is awkward in a path.
func exampleTags(pinned []string, spec exampleDistroSpec, f exampleFlavor) ([]string, error) {
	tags := map[string]bool{}
	for _, tag := range pinned {
		tags[tag] = true
	}

	minors, err := os.ReadDir(f.dir)
	if os.IsNotExist(err) {
		// A flavor with no examples yet renders the pinned tags, and grows from there.
		return sortedTags(tags)
	}
	if err != nil {
		return nil, err
	}
	for _, minor := range minors {
		if !minor.IsDir() || !exampleMinorPattern.MatchString(minor.Name()) {
			continue
		}

		entries, err := os.ReadDir(filepath.Join(f.dir, minor.Name()))
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			tag := strings.Replace(e.Name(), "-"+spec.name, "+"+spec.name, 1)
			if _, err := tagVersion(tag); err != nil {
				// Not a version directory -- nothing to render.
				continue
			}
			tags[tag] = true
		}
	}

	return sortedTags(tags)
}

// sortedTags flattens a tag set newest first.
func sortedTags(tags map[string]bool) ([]string, error) {
	out := slices.Collect(maps.Keys(tags))
	return out, sortTagsDesc(out)
}

// sortTagsDesc orders tags newest first, so generated output and the lines it prints read
// the way a reader scanning for the current release expects.
func sortTagsDesc(tags []string) error {
	var sortErr error
	slices.SortFunc(tags, func(a, b string) int {
		va, err := tagVersion(a)
		if err != nil {
			sortErr = err
		}
		vb, err := tagVersion(b)
		if err != nil {
			sortErr = err
		}
		if c := slices.Compare(vb[:], va[:]); c != 0 {
			return c
		}
		return strings.Compare(b, a) // same version, order by the rke2rN/k3sN revision
	})
	return sortErr
}

// writeExample renders one tag into <flavor dir>/<minor>/<tag>/distro.yaml, creating the
// minor line directory if this is its first example, and returns the path it wrote.
//
// It returns "" for a build upstream no longer publishes, having removed any example
// already on disk for it. Rancher supersedes an rke2rN with the next revision and drops the
// old RPMs -- v1.35.0+rke2r2 and v1.35.3+rke2r2 both gave way to an rke2r3 -- and an example
// pointing at RPM URLs that 404 cannot be built, so keeping it only invites someone to try.
// The git tag surviving is what makes these renderable in the first place, so it is the
// distro's own artifacts that get checked.
func writeExample(tmpl *template.Template, repoURL, tag string, spec exampleDistroSpec, f exampleFlavor, sums *exampleShasums) (string, error) {
	v, err := newExampleVersion(tag, spec, f)
	if err != nil {
		return "", err
	}

	minor, err := tagMinor(tag)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(f.dir, minor, "v"+v.Version)

	if spec.probe != nil {
		published, err := sums.published(spec.probe(v))
		if err != nil {
			return "", err
		}
		if !published {
			return "", removeExample(dir)
		}
	}

	// Only now, once the build is known to be installable, spend the release fetches.
	if err := v.fetchImages(repoURL, spec, f); err != nil {
		return "", err
	}
	if spec.fetch != nil {
		if err := spec.fetch(&v, repoURL); err != nil {
			return "", err
		}
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, v); err != nil {
		return "", err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	path := filepath.Join(dir, "distro.yaml")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// newExampleVersion derives every version-varying field of an example from one tag and the
// flavor being rendered, except what has to be fetched -- the image lists, and whatever the
// distro's own fetch hook adds.
func newExampleVersion(tag string, spec exampleDistroSpec, f exampleFlavor) (exampleVersion, error) {
	semver, err := tagVersion(tag)
	if err != nil {
		return exampleVersion{}, err
	}

	// v1.36.4+rke2r1 -> 1.36.4-rke2r1 / 1.36.4~rke2r1 / v1.36.4%2Brke2r1
	trimmed := strings.TrimPrefix(tag, "v")
	v := exampleVersion{
		Version:           strings.ReplaceAll(trimmed, "+", "-"),
		Minor:             fmt.Sprintf("%d.%d", semver[0], semver[1]),
		RPMVersion:        strings.ReplaceAll(trimmed, "+", "~"),
		TagURL:            strings.ReplaceAll(tag, "+", "%2B"),
		CNI:               f.cni,
		ReplacesKubeProxy: f.replacesKubeProxy,
	}
	if spec.derive != nil {
		spec.derive(&v)
	}

	return v, nil
}

// fetchImages fills in the image lists from the release's published airgap manifests.
func (v *exampleVersion) fetchImages(repoURL string, spec exampleDistroSpec, f exampleFlavor) error {
	var err error
	if v.CoreImages, err = fetchImageList(repoURL, v.TagURL, spec.coreImages); err != nil {
		return err
	}
	if f.imageList == "" {
		return nil
	}
	if v.CNIImages, err = fetchImageList(repoURL, v.TagURL, f.imageList); err != nil {
		return err
	}
	return nil
}

// removeExample deletes an example directory, and the minor line directory with it if that
// leaves it empty. Missing is fine -- the point is that it is gone.
func removeExample(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	// Fails while the line still holds examples, which is exactly when it should stay.
	os.Remove(filepath.Dir(dir))
	return nil
}

// fetchImageList downloads one of a release's airgap image manifests and returns its
// non-empty lines, in file order.
func fetchImageList(repoURL, tagURL, asset string) ([]string, error) {
	images, err := fetchReleaseLines(repoURL, tagURL, asset)
	if err != nil {
		return nil, err
	}
	if len(images) == 0 {
		return nil, fmt.Errorf("%s/%s is empty", tagURL, asset)
	}
	return images, nil
}

// fetchReleaseLines downloads one of a release's text assets and returns its non-empty
// lines, in file order.
func fetchReleaseLines(repoURL, tagURL, asset string) ([]string, error) {
	url := fmt.Sprintf("%s/releases/download/%s/%s", strings.TrimSuffix(repoURL, "/"), tagURL, asset)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: %s", url, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", url, err)
	}

	var lines []string
	for line := range strings.SplitSeq(string(body), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}
