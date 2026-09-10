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

// The multi-architecture flavors exist to show what a package covering more than one
// architecture looks like, not to cover every release: one current minor line is enough to
// read, and keeps the arm64 artifacts the shasum cache has to hold down to a handful.
var (
	exampleMultiArches = []string{"amd64", "arm64"}
	exampleMultiMinors = []string{"v1_36"}
)

// exampleRPMArches maps a Go architecture to the name rpm.rancher.io publishes it under.
var exampleRPMArches = map[string]string{
	"amd64": "x86_64",
	"arm64": "aarch64",
}

// exampleFlavor is one build of a distro the template renders. Flavors are usually named for
// their CNI and written to their own directory, since the CNI choice reaches further than the
// image list: cilium replaces kube-proxy and is configured through a HelmChartConfig
// manifest, while canal and flannel run alongside kube-proxy and need no manifest. The
// multi-architecture flavors are the exception: they are named for what they demonstrate
// rather than for a CNI, since the CNI is not what makes them worth having.
type exampleFlavor struct {
	cni               string   // cilium -- what the template configures
	name              string   // multi -- what follows the distro in metadata.name; the CNI when empty
	dir               string   // example/rke2-cilium -- where its examples are written
	imageLists        []string // rke2-images-cilium.linux-amd64.txt -- the airgap manifests it adds to the core one
	replacesKubeProxy bool     // whether the CNI takes over from kube-proxy
	encryption        bool     // whether the CNI encrypts pod-to-pod traffic
	cloudProvider     string   // rancher-vsphere -- the bundled cloud provider it selects; none when empty
	arches            []string // the architectures its examples target; amd64 alone when empty
	minors            []string // v1_36 -- the minor lines it renders, all of them when empty
}

// flavorName is what the example's metadata.name ends in.
func (f exampleFlavor) flavorName() string {
	if f.name != "" {
		return f.name
	}
	return f.cni
}

// architectures is what the flavor's examples target. Most examples are amd64 only, so that
// is what an unset list means rather than nothing at all.
func (f exampleFlavor) architectures() []string {
	if len(f.arches) > 0 {
		return f.arches
	}
	return []string{"amd64"}
}

// covers reports whether a tag belongs to a minor line this flavor renders. A flavor that
// names no lines renders every tag it is given.
func (f exampleFlavor) covers(tag string) (bool, error) {
	if len(f.minors) == 0 {
		return true, nil
	}
	minor, err := tagMinor(tag)
	if err != nil {
		return false, err
	}
	return slices.Contains(f.minors, minor), nil
}

// filterFlavorTags drops the tags a flavor does not render, keeping the order it was given.
func filterFlavorTags(tags []string, f exampleFlavor) ([]string, error) {
	var out []string
	for _, tag := range tags {
		covered, err := f.covers(tag)
		if err != nil {
			return nil, err
		}
		if covered {
			out = append(out, tag)
		}
	}
	return out, nil
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
				imageLists:        []string{"rke2-images-cilium.linux-amd64.txt"},
				replacesKubeProxy: true,
			},
			{
				cni:               "cilium",
				name:              "multi-cni-cilium",
				dir:               "example/rke2-multi-cni-cilium",
				imageLists:        []string{"rke2-images-cilium.linux-amd64.txt"},
				arches:            exampleMultiArches,
				minors:            exampleMultiMinors,
				replacesKubeProxy: true,
			},
			{
				cni:               "cilium",
				name:              "cilium-vsphere",
				dir:               "example/rke2-cilium-vsphere",
				imageLists:        []string{"rke2-images-cilium.linux-amd64.txt", "rke2-images-vsphere.linux-amd64.txt"},
				replacesKubeProxy: true,
				cloudProvider:     "rancher-vsphere",
			},
			{
				cni:               "cilium",
				name:              "cilium-wireguard",
				dir:               "example/rke2-cilium-wireguard",
				imageLists:        []string{"rke2-images-cilium.linux-amd64.txt"},
				minors:            exampleMultiMinors,
				replacesKubeProxy: true,
				encryption:        true,
			},
			{
				cni:        "canal",
				dir:        "example/rke2-canal",
				imageLists: []string{"rke2-images-canal.linux-amd64.txt"},
			},
			{
				cni:        "canal",
				name:       "multi-cni-canal",
				dir:        "example/rke2-multi-cni-canal",
				imageLists: []string{"rke2-images-canal.linux-amd64.txt"},
				arches:     exampleMultiArches,
				minors:     exampleMultiMinors,
			},
		},
		derive: func(v *exampleVersion) {
			v.SelinuxRPM = exampleRKE2SelinuxRPM
			for i := range v.Arches {
				a := &v.Arches[i]
				a.CommonRPM = exampleRKE2RPM("common", v.Minor, v.RPMVersion, a.RPMArch)
				a.ServerRPM = exampleRKE2RPM("server", v.Minor, v.RPMVersion, a.RPMArch)
				a.AgentRPM = exampleRKE2RPM("agent", v.Minor, v.RPMVersion, a.RPMArch)
				a.Tarball = fmt.Sprintf("rke2.linux-%s.tar.gz", a.Arch)
			}
		},
		// RKE2 installs from RPMs, and Rancher removes an rke2rN's RPMs once the next
		// revision supersedes it, long before the git tag goes anywhere. Every architecture
		// of a build is published and pulled together, so the first one answers for all.
		probe: func(v exampleVersion) string { return v.Arches[0].CommonRPM },
	},
	{
		name:       "k3s",
		template:   "magefiles/templates/k3s-distro.yaml.tmpl",
		coreImages: "k3s-images.txt",
		// k3s ships its CNI in the binary, so flannel is what a stock k3s runs, and its
		// images are already in k3s-images.txt.
		flavors: []exampleFlavor{
			{cni: "flannel", dir: "example/k3s-flannel"},
			{
				cni:    "flannel",
				name:   "multi",
				dir:    "example/k3s-multi",
				arches: exampleMultiArches,
				minors: exampleMultiMinors,
			},
		},
		derive: func(v *exampleVersion) {
			v.SelinuxRPM = exampleK3sSelinuxRPM
			for i := range v.Arches {
				a := &v.Arches[i]
				a.BinaryURL = fmt.Sprintf("https://github.com/k3s-io/k3s/releases/download/%s/%s",
					v.TagURL, exampleK3sBinary(a.Arch))
			}
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
	Name              string   // flannel -- what the example's metadata.name ends in
	ReplacesKubeProxy bool     // whether to set disable-kube-proxy
	Encryption        bool     // whether to turn on the CNI's transparent encryption
	CloudProvider     string   // rancher-vsphere -- what cloud-provider-name selects, when the flavor sets one
	CoreImages        []string // the release's own image manifest
	CNIImages         []string // the flavor's own manifests, concatenated, when it has any

	// Arches is every architecture the example targets, and the files each of them installs.
	// MultiArch says whether there is more than one, which is what decides both how the
	// example declares its architectures and whether its files need an arch selector at all.
	Arches    []exampleArch
	MultiArch bool

	// The selinux policy RPM both distros share, and the one file neither publishes per
	// architecture. It is built here rather than in the templates so that the URL checked
	// against upstream is the same one the example carries.
	SelinuxRPM string
}

// exampleArch is one architecture's share of an example: the files a distro publishes once
// per architecture, at the URLs that architecture publishes them under. A single-architecture
// flavor has one of these, so the templates range over them either way.
type exampleArch struct {
	Arch    string // amd64 -- what a file's arch selector names
	RPMArch string // x86_64 -- what rpm.rancher.io calls the same architecture

	// RKE2 installs from RPMs, plus a release tarball its binaries are extracted from.
	CommonRPM string
	ServerRPM string
	AgentRPM  string
	Tarball   string // rke2.linux-amd64.tar.gz

	// k3s installs a single binary, whose digest the release publishes for us.
	BinaryURL string
	BinarySHA string
}

// exampleRKE2RPM is the rpm.rancher.io URL of one of a build's versioned RPMs, for one
// architecture.
func exampleRKE2RPM(pkg, minor, rpmVersion, rpmArch string) string {
	return fmt.Sprintf("https://rpm.rancher.io/rke2/latest/%s/centos/9/%s/rke2-%s-%s-0.el9.%s.rpm",
		minor, rpmArch, pkg, rpmVersion, rpmArch)
}

// exampleK3sBinary is the name a k3s release publishes its binary for an architecture under.
// amd64 is the unsuffixed one, every other architecture carries its own name.
func exampleK3sBinary(arch string) string {
	if arch == "amd64" {
		return "k3s"
	}
	return "k3s-" + arch
}

// fetchK3sBinarySHA reads each architecture's k3s binary digest out of the release's own
// checksum file for that architecture. Every k3s release publishes one per architecture, so
// the binaries themselves -- tens of megabytes, and named plainly enough that every version
// would collide in the cache -- never have to be downloaded to be verified.
func fetchK3sBinarySHA(v *exampleVersion, repoURL string) error {
	for i := range v.Arches {
		a := &v.Arches[i]
		asset := fmt.Sprintf("sha256sum-%s.txt", a.Arch)
		binary := exampleK3sBinary(a.Arch)

		lines, err := fetchReleaseLines(repoURL, v.TagURL, asset)
		if err != nil {
			return err
		}
		sum, err := k3sBinarySHA(lines, binary)
		if err != nil {
			return fmt.Errorf("%s for %s: %w", asset, v.TagURL, err)
		}
		a.BinarySHA = sum
	}
	return nil
}

// k3sBinarySHA picks one binary's digest out of a release checksum file's lines.
func k3sBinarySHA(lines []string, binary string) (string, error) {
	for _, line := range lines {
		// "<sha256>  k3s", alongside the airgap tarballs.
		if sum, name, ok := strings.Cut(line, " "); ok && strings.TrimSpace(name) == binary {
			return sum, nil
		}
	}
	return "", fmt.Errorf("no %s entry", binary)
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
		return flavorTags(tags, f)
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

	return flavorTags(tags, f)
}

// flavorTags flattens a tag set newest first, dropping the minor lines the flavor does not
// render.
func flavorTags(tags map[string]bool, f exampleFlavor) ([]string, error) {
	out := slices.Collect(maps.Keys(tags))
	if err := sortTagsDesc(out); err != nil {
		return nil, err
	}
	return filterFlavorTags(out, f)
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
		Name:              f.flavorName(),
		ReplacesKubeProxy: f.replacesKubeProxy,
		Encryption:        f.encryption,
		CloudProvider:     f.cloudProvider,
	}
	for _, arch := range f.architectures() {
		rpmArch, ok := exampleRPMArches[arch]
		if !ok {
			return exampleVersion{}, fmt.Errorf("no rpm architecture known for %s", arch)
		}
		v.Arches = append(v.Arches, exampleArch{Arch: arch, RPMArch: rpmArch})
	}
	v.MultiArch = len(v.Arches) > 1

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
	for _, asset := range f.imageLists {
		images, err := fetchImageList(repoURL, v.TagURL, asset)
		if err != nil {
			return err
		}
		v.CNIImages = append(v.CNIImages, images...)
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
