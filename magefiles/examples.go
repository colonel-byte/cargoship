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
	// exampleDistro is the pins.json entry, and the distro name embedded in a tag, that
	// examples are rendered from.
	exampleDistro   = "rke2"
	exampleTemplate = "magefiles/templates/rke2-distro.yaml.tmpl"

	// exampleCoreImages is the airgap image manifest every flavor starts from. Each flavor
	// adds its CNI's manifest on top. They are release assets, so an example's image list is
	// exactly what that release ships rather than something reconstructed here.
	exampleCoreImages = "rke2-images-core.linux-amd64.txt"

	// exampleSelinuxRPM is shared by every version, unlike the rke2-common/server/agent RPMs
	// that carry the build's own version.
	exampleSelinuxRPM = "https://rpm.rancher.io/rke2/latest/common/centos/9/noarch/rke2-selinux-0.22-1.el9.noarch.rpm"
)

// exampleFlavor is one RKE2 build the template renders. The flavors differ only in their
// CNI, but that choice reaches further than the image list: cilium replaces kube-proxy and
// is configured through a HelmChartConfig manifest, while canal runs alongside kube-proxy
// and needs no manifest of its own.
type exampleFlavor struct {
	cni               string // cilium -- spec.config.engine.config.cni, and the flavor's name
	dir               string // example/rke2-cilium -- where its examples are written
	imageList         string // rke2-images-cilium.linux-amd64.txt -- its CNI's airgap manifest
	replacesKubeProxy bool   // whether the CNI takes over from kube-proxy
}

// exampleFlavors is every flavor the example targets render, each into its own directory.
var exampleFlavors = []exampleFlavor{
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
}

// exampleMinorPattern matches the minor line directories examples are grouped under
// (example/rke2-cilium/v1_35), so anything else at that level is left alone. The naming
// matches thirdparty-src/rke2/ and src/pkg/engineconfig/gen/rke2/.
var exampleMinorPattern = regexp.MustCompile(`^v[0-9]+_[0-9]+$`)

// exampleVersion is what magefiles/templates/rke2-distro.yaml.tmpl renders against. Every
// field is derived from one pinned tag and the flavor being rendered, so a whole example
// follows from pins.json plus the flavor.
type exampleVersion struct {
	Version           string   // 1.36.4-rke2r1 -- metadata.version and spec.version
	Minor             string   // 1.36 -- the channel in the rpm.rancher.io paths
	RPMVersion        string   // 1.36.4~rke2r1 -- RPM file names
	TagURL            string   // v1.36.4%2Brke2r1 -- the release download path segment
	CNI               string   // cilium
	ReplacesKubeProxy bool     // whether to set disable-kube-proxy
	CoreImages        []string // rke2-images-core.linux-amd64.txt
	CNIImages         []string // the flavor's CNI image manifest

	// The RPMs the example installs. They are built here rather than in the template so
	// that the same URLs the example carries are the ones checked against rpm.rancher.io.
	CommonRPM  string
	SelinuxRPM string
	ServerRPM  string
	AgentRPM   string
}

// parseExampleTemplate parses the example template, wiring in the sha256 function its remote
// file entries are hashed with.
func parseExampleTemplate(sums *exampleShasums) (*template.Template, error) {
	tmpl, err := template.New(filepath.Base(exampleTemplate)).
		Funcs(template.FuncMap{"sha256": sums.get}).
		ParseFiles(exampleTemplate)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", exampleTemplate, err)
	}
	return tmpl, nil
}

// exampleTags is every tag one flavor renders an example for: the pinned ones plus whatever
// that flavor already has on disk, newest first. Examples live at
// <flavor dir>/<minor>/<tag>/distro.yaml, and a tag directory is named for its tag with the
// "+" swapped for a "-", since "+" is awkward in a path.
func exampleTags(pinned []string, f exampleFlavor) ([]string, error) {
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
			tag := strings.Replace(e.Name(), "-"+exampleDistro, "+"+exampleDistro, 1)
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
		return strings.Compare(b, a) // same version, order by the rke2rN revision
	})
	return sortErr
}

// writeExample renders one tag into <flavor dir>/<minor>/<tag>/distro.yaml, creating the
// minor line directory if this is its first example, and returns the path it wrote.
//
// It returns "" for a build rpm.rancher.io no longer publishes, having removed any example
// already on disk for it. Rancher supersedes an rke2rN with the next revision and drops the
// old RPMs -- v1.35.0+rke2r2 and v1.35.3+rke2r2 both gave way to an rke2r3 -- and an example
// pointing at RPM URLs that 404 cannot be built, so keeping it only invites someone to try.
// The git tag surviving is what makes these renderable in the first place, so the RPMs are
// what gets checked.
func writeExample(tmpl *template.Template, repoURL, tag string, f exampleFlavor, sums *exampleShasums) (string, error) {
	v, err := newExampleVersion(tag, f)
	if err != nil {
		return "", err
	}

	minor, err := tagMinor(tag)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(f.dir, minor, "v"+v.Version)

	published, err := sums.published(v.CommonRPM)
	if err != nil {
		return "", err
	}
	if !published {
		return "", removeExample(dir)
	}

	// Only now, once the build is known to be installable, spend the image manifest fetches.
	if err := v.fetchImages(repoURL, f); err != nil {
		return "", err
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
// flavor being rendered, except the image lists -- those are fetched by fetchImages.
func newExampleVersion(tag string, f exampleFlavor) (exampleVersion, error) {
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
		SelinuxRPM:        exampleSelinuxRPM,
	}
	v.CommonRPM = exampleRPM("common", v.Minor, v.RPMVersion)
	v.ServerRPM = exampleRPM("server", v.Minor, v.RPMVersion)
	v.AgentRPM = exampleRPM("agent", v.Minor, v.RPMVersion)

	return v, nil
}

// exampleRPM is the rpm.rancher.io URL of one of a build's versioned RPMs.
func exampleRPM(pkg, minor, rpmVersion string) string {
	return fmt.Sprintf("https://rpm.rancher.io/rke2/latest/%s/centos/9/x86_64/rke2-%s-%s-0.el9.x86_64.rpm",
		minor, pkg, rpmVersion)
}

// fetchImages fills in the image lists from the release's published airgap manifests.
func (v *exampleVersion) fetchImages(repoURL string, f exampleFlavor) error {
	var err error
	if v.CoreImages, err = fetchImageList(repoURL, v.TagURL, exampleCoreImages); err != nil {
		return err
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

	var images []string
	for line := range strings.SplitSeq(string(body), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			images = append(images, line)
		}
	}
	if len(images) == 0 {
		return nil, fmt.Errorf("%s is empty", url)
	}
	return images, nil
}
