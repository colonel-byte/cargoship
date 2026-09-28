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

	"github.com/colonel-byte/cargoship/magefiles/pkg/example/arch"
)

const (
	// The selinux policy RPMs are shared by every version of their distro, unlike the RPMs
	// and binaries that carry the build's own version.
	rke2SelinuxRPM = "https://rpm.rancher.io/rke2/latest/common/centos/9/noarch/rke2-selinux-0.22-1.el9.noarch.rpm"
	k3sSelinuxRPM  = "https://rpm.rancher.io/k3s/latest/common/centos/9/noarch/k3s-selinux-1.6-1.el9.noarch.rpm"
)

// distroSpec is everything that differs between the distros examples are rendered
// for. The distros agree on more than they differ on -- a tag, a template, image manifests
// published with the release -- so what varies is pushed into the three hooks rather than
// into two copies of the rendering itself.
type distroSpec struct {
	name     string // the pins.json entry, and the distro name embedded in a tag
	template string
	flavors  []flavor

	// coreImages is the release's own image manifest: everything the distro ships before a
	// flavor adds its CNI's images. RKE2 splits the two, k3s publishes one combined list.
	// Either way an example's images are what that release ships, not a list rebuilt here.
	coreImages string

	// derive fills in the fields only this distro has, from the version already parsed.
	// Offline, so probe has something to check before anything is fetched.
	derive func(v *version)

	// probe returns the URL whose absence means this build can no longer be installed, and
	// so should not be kept as an example. Empty means the distro publishes nothing that
	// disappears out from under a live git tag.
	probe func(v version) string

	// fetch pulls anything else the template needs that only the release can answer. May be
	// nil.
	fetch func(v *version, repoURL string) error
}

// rke2Values and k3sValues are the values files a flavor of each distro ships:
// the knobs a cluster turns without rebuilding the package, and the schema they are checked
// against. There is one pair per distro rather than one per flavor because a package carries a
// single schema, so everything a flavor exposes has to be described in the same document. The
// templates branch on the flavor for the parts that are not common to all of them.
var (
	rke2Values = []string{
		"magefiles/templates/rke2/values.yaml.tmpl",
		"magefiles/templates/rke2/values.schema.json.tmpl",
	}
	k3sValues = []string{
		"magefiles/templates/k3s/values.yaml.tmpl",
		"magefiles/templates/k3s/values.schema.json.tmpl",
	}
)

// distros is every distro the example targets render.
var distros = []distroSpec{
	{
		name:       "rke2",
		template:   "magefiles/templates/rke2-distro.yaml.tmpl",
		coreImages: "rke2-images-core.linux-amd64.txt",
		flavors: []flavor{
			{
				cni:               "cilium",
				name:              "multi-cni-cilium",
				dir:               "example/rke2-multi-cni-cilium",
				imageLists:        []string{"rke2-images-cilium.linux-amd64.txt"},
				arches:            arch.Multi,
				minors:            multiMinors,
				replacesKubeProxy: true,
				values:            rke2Values,
			},
			{
				cni:               "cilium",
				name:              "cilium-vsphere",
				dir:               "example/rke2-cilium-vsphere",
				imageLists:        []string{"rke2-images-cilium.linux-amd64.txt", "rke2-images-vsphere.linux-amd64.txt"},
				replacesKubeProxy: true,
				cloudProvider:     "rancher-vsphere",
				values:            rke2Values,
			},
			{
				cni:        "canal",
				name:       "multi-cni-canal",
				dir:        "example/rke2-multi-cni-canal",
				imageLists: []string{"rke2-images-canal.linux-amd64.txt"},
				arches:     arch.Multi,
				minors:     multiMinors,
				values:     rke2Values,
			},
		},
		derive: func(v *version) {
			v.SelinuxRPM = rke2SelinuxRPM
			for i := range v.Arches {
				a := &v.Arches[i]
				a.CommonRPM = rke2RPM("common", v.Minor, v.RPMVersion, a.RPMArch)
				a.ServerRPM = rke2RPM("server", v.Minor, v.RPMVersion, a.RPMArch)
				a.AgentRPM = rke2RPM("agent", v.Minor, v.RPMVersion, a.RPMArch)
				a.Tarball = fmt.Sprintf("rke2.linux-%s.tar.gz", a.Arch)
			}
		},
		// RKE2 installs from RPMs, and Rancher removes an rke2rN's RPMs once the next
		// revision supersedes it, long before the git tag goes anywhere. Every architecture
		// of a build is published and pulled together, so the first one answers for all.
		probe: func(v version) string { return v.Arches[0].CommonRPM },
	},
	{
		name:       "k3s",
		template:   "magefiles/templates/k3s-distro.yaml.tmpl",
		coreImages: "k3s-images.txt",
		// k3s ships its CNI in the binary, so flannel is what a stock k3s runs, and its
		// images are already in k3s-images.txt.
		flavors: []flavor{
			{cni: "flannel", dir: "example/k3s-flannel", values: k3sValues},
			{
				cni:    "flannel",
				name:   "multi",
				dir:    "example/k3s-multi",
				arches: arch.Multi,
				minors: multiMinors,
				values: k3sValues,
			},
		},
		derive: func(v *version) {
			v.SelinuxRPM = k3sSelinuxRPM
			for i := range v.Arches {
				a := &v.Arches[i]
				a.BinaryURL = fmt.Sprintf("https://github.com/k3s-io/k3s/releases/download/%s/%s",
					v.TagURL, k3sBinary(a.Arch))
			}
		},
		fetch: fetchK3sBinarySHA,
	},
}
