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

// This file declares every distro the example targets render, and the pieces only one distro
// needs: the URLs it publishes its artifacts under, and the fetch hook that fills in what only
// its releases can answer. The flavors each distro lists are defined in flavor.go.

package examples

import (
	"fmt"
	"strings"
)

const (
	// The selinux policy RPMs are shared by every version of their distro, unlike the RPMs
	// and binaries that carry the build's own version.
	exampleRKE2SelinuxRPM = "https://rpm.rancher.io/rke2/latest/common/centos/9/noarch/rke2-selinux-0.22-1.el9.noarch.rpm"
	exampleK3sSelinuxRPM  = "https://rpm.rancher.io/k3s/latest/common/centos/9/noarch/k3s-selinux-1.6-1.el9.noarch.rpm"
)

// The multi-architecture flavors exist to show what a package covering more than one
// architecture looks like, not to cover every release: the current minor lines are enough to
// read, and keep the arm64 artifacts the shasum cache has to hold down to a handful.
//
// Every line the examples render is now inside that window -- see exampleMinorFloor -- so this
// list and the rendered lines currently coincide. It stays a list of its own because the two
// answer different questions: which lines are rendered at all, and which are worth paying
// arm64 artifacts for.
var (
	exampleMultiArches = []string{"amd64", "arm64"}
	exampleMultiMinors = []string{
		"v1_35",
		"v1_36",
		"v1_37",
	}
)

// exampleRPMArches maps a Go architecture to the name rpm.rancher.io publishes it under.
var exampleRPMArches = map[string]string{
	"amd64": "x86_64",
	"arm64": "aarch64",
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

// exampleRKE2Values and exampleK3sValues are the values files a flavor of each distro ships:
// the knobs a cluster turns without rebuilding the package, and the schema they are checked
// against. There is one pair per distro rather than one per flavor because a package carries a
// single schema, so everything a flavor exposes has to be described in the same document. The
// templates branch on the flavor for the parts that are not common to all of them.
var (
	exampleRKE2Values = []string{
		"magefiles/templates/rke2/values.yaml.tmpl",
		"magefiles/templates/rke2/values.schema.json.tmpl",
	}
	exampleK3sValues = []string{
		"magefiles/templates/k3s/values.yaml.tmpl",
		"magefiles/templates/k3s/values.schema.json.tmpl",
	}
)

// exampleDistros is every distro the example targets render.
var exampleDistros = []exampleDistroSpec{
	{
		name:       "rke2",
		template:   "magefiles/templates/rke2-distro.yaml.tmpl",
		coreImages: "rke2-images-core.linux-amd64.txt",
		flavors: []exampleFlavor{
			{
				cni:               "cilium",
				name:              "multi-cni-cilium",
				dir:               "example/rke2-multi-cni-cilium",
				imageLists:        []string{"rke2-images-cilium.linux-amd64.txt"},
				arches:            exampleMultiArches,
				minors:            exampleMultiMinors,
				replacesKubeProxy: true,
				values:            exampleRKE2Values,
			},
			{
				cni:               "cilium",
				name:              "cilium-vsphere",
				dir:               "example/rke2-cilium-vsphere",
				imageLists:        []string{"rke2-images-cilium.linux-amd64.txt", "rke2-images-vsphere.linux-amd64.txt"},
				replacesKubeProxy: true,
				cloudProvider:     "rancher-vsphere",
				values:            exampleRKE2Values,
			},
			{
				cni:        "canal",
				name:       "multi-cni-canal",
				dir:        "example/rke2-multi-cni-canal",
				imageLists: []string{"rke2-images-canal.linux-amd64.txt"},
				arches:     exampleMultiArches,
				minors:     exampleMultiMinors,
				values:     exampleRKE2Values,
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
				a.TarballURL = fmt.Sprintf("https://github.com/rancher/rke2/releases/download/%s/%s",
					v.TagURL, a.Tarball)
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
			{cni: "flannel", dir: "example/k3s-flannel", values: exampleK3sValues},
			{
				cni:    "flannel",
				name:   "multi",
				dir:    "example/k3s-multi",
				arches: exampleMultiArches,
				minors: exampleMultiMinors,
				values: exampleK3sValues,
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
