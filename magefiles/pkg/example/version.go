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

package example

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/colonel-byte/cargoship/magefiles/pkg/engine/pins"
	"github.com/colonel-byte/cargoship/magefiles/pkg/example/arch"
	"github.com/colonel-byte/cargoship/magefiles/pkg/example/releaselines"
	"github.com/colonel-byte/cargoship/pkg/engineconfig/gen"
)

// version is what the templates render against. Every field is derived from one tag
// and the flavor being rendered, so a whole example follows from pins.json plus the flavor.
type version struct {
	Version           string   // 1.36.4-rke2r1 -- metadata.version and spec.version
	Minor             string   // 1.36 -- the channel in the rpm.rancher.io paths
	RPMVersion        string   // 1.36.4~rke2r1 -- RPM file names
	TagURL            string   // v1.36.4%2Brke2r1 -- the release download path segment
	CNI               string   // cilium
	Name              string   // flannel -- what the example's metadata.name ends in
	ReplacesKubeProxy bool     // whether to set disable-kube-proxy
	CloudProvider     string   // rancher-vsphere -- what cloud-provider-name selects, when the flavor sets one
	CoreImages        []string // the release's own image manifest
	CNIImages         []string // the flavor's own manifests, concatenated, when it has any

	// Addons is every packaged component this distro/version advertises under `disable:`,
	// read from the vocabulary mage generate:engineConfig extracts from the engine's own
	// source. A flavor's values schema offers these as suggestions for addons.disabled, so
	// what a package proposes is what that build actually packages -- v1.37 of RKE2 knows
	// rke2-gateway-api-crd and v1.34 does not, and each version's schema says so. The
	// engine's own help text is not exhaustive (rke2 bundles rke2-runtimeclasses without
	// listing it), so the schema suggests rather than restricts.
	Addons []string

	// Arches is every architecture the example targets, and the files each of them installs.
	// MultiArch says whether there is more than one, which is what decides both how the
	// example declares its architectures and whether its files need an arch selector at all.
	Arches    []versionArch
	MultiArch bool

	// The selinux policy RPM both distros share, and the one file neither publishes per
	// architecture. It is built here rather than in the templates so that the URL checked
	// against upstream is the same one the example carries.
	SelinuxRPM string
}

// versionArch is one architecture's share of an example: the files a distro publishes once
// per architecture, at the URLs that architecture publishes them under. A single-architecture
// flavor has one of these, so the templates range over them either way.
type versionArch struct {
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

// rke2RPM is the rpm.rancher.io URL of one of a build's versioned RPMs, for one
// architecture.
func rke2RPM(pkg, minor, rpmVersion, rpmArch string) string {
	return fmt.Sprintf("https://rpm.rancher.io/rke2/latest/%s/centos/9/%s/rke2-%s-%s-0.el9.%s.rpm",
		minor, rpmArch, pkg, rpmVersion, rpmArch)
}

// k3sBinary is the name a k3s release publishes its binary for an architecture under.
// amd64 is the unsuffixed one, every other architecture carries its own name.
func k3sBinary(arch string) string {
	if arch == "amd64" {
		return "k3s"
	}
	return "k3s-" + arch
}

// fetchK3sBinarySHA reads each architecture's k3s binary digest out of the release's own
// checksum file for that architecture. Every k3s release publishes one per architecture, so
// the binaries themselves -- tens of megabytes, and named plainly enough that every version
// would collide in the cache -- never have to be downloaded to be verified.
func fetchK3sBinarySHA(v *version, repoURL string) error {
	for i := range v.Arches {
		a := &v.Arches[i]
		asset := fmt.Sprintf("sha256sum-%s.txt", a.Arch)
		binary := k3sBinary(a.Arch)

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

// flavor being rendered, except what has to be fetched -- the image lists, and whatever the
// distro's own fetch hook adds.
func newVersion(tag string, spec distroSpec, f flavor) (version, error) {
	semver, err := pins.TagVersion(tag)
	if err != nil {
		return version{}, err
	}

	// v1.36.4+rke2r1 -> 1.36.4-rke2r1 / 1.36.4~rke2r1 / v1.36.4%2Brke2r1
	trimmed := strings.TrimPrefix(tag, "v")
	v := version{
		Version:           strings.ReplaceAll(trimmed, "+", "-"),
		Minor:             fmt.Sprintf("%d.%d", semver[0], semver[1]),
		RPMVersion:        strings.ReplaceAll(trimmed, "+", "~"),
		TagURL:            strings.ReplaceAll(tag, "+", "%2B"),
		CNI:               f.cni,
		Name:              f.flavorName(),
		ReplacesKubeProxy: f.replacesKubeProxy,
		CloudProvider:     f.cloudProvider,
	}
	for _, goArch := range f.architectures() {
		rpmArch, ok := arch.RPM[goArch]
		if !ok {
			return version{}, fmt.Errorf("no rpm architecture known for %s", goArch)
		}
		v.Arches = append(v.Arches, versionArch{Arch: goArch, RPMArch: rpmArch})
	}
	v.MultiArch = len(v.Arches) > 1

	if spec.derive != nil {
		spec.derive(&v)
	}

	// Missing only for a version whose source was never pulled into this build, which
	// writeValues turns into an error rather than a schema that accepts nothing.
	if entry, ok := gen.Lookup(spec.name, v.Version); ok {
		v.Addons = entry.Addons
	}

	return v, nil
}

// fetchImages fills in the image lists from the release's published airgap manifests.
func (v *version) fetchImages(repoURL string, spec distroSpec, f flavor) error {
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

// removeDir deletes an example directory, and the minor line directory with it if that
// leaves it empty. Missing is fine -- the point is that it is gone.
func removeDir(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	// Fails while the line still holds examples, which is exactly when it should stay.
	os.Remove(filepath.Dir(dir)) //nolint:errcheck // a non-empty minor line is the normal case
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

// fetchReleaseLines returns one of a release's text assets as its non-empty lines, in file
// order, reading the cache before it reaches for the network and recording what it fetched.
// A release's assets do not change under a tag, so the second render of a version is free;
// CARGOSHIP_EXAMPLES_NO_CACHE covers the case where one did change.
func fetchReleaseLines(repoURL, tagURL, asset string) ([]string, error) {
	url := fmt.Sprintf("%s/releases/download/%s/%s", strings.TrimSuffix(repoURL, "/"), tagURL, asset)

	cache := releaselines.Shared()
	if lines, ok := cache.Lookup(url); ok {
		return lines, nil
	}

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body, nothing to do with a close error

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: %s", url, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", url, err)
	}

	lines := releaselines.Split(body)
	cache.Store(url, lines)
	return lines, nil
}
