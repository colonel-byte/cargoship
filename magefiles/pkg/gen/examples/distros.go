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
	"regexp"
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

	// fetch pulls anything else the template needs that only the release can answer. It is
	// handed the shasum cache, since a digest a release publishes belongs in it rather than
	// being refetched. May be nil.
	fetch func(v *exampleVersion, repoURL string, sums *exampleShasums) error
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
				imageLists:        []exampleImageList{{asset: "rke2-images-cilium.linux-amd64.txt", charts: []string{"rke2-cilium"}}},
				arches:            exampleMultiArches,
				minors:            exampleMultiMinors,
				replacesKubeProxy: true,
				values:            exampleRKE2Values,
			},
			{
				cni:  "cilium",
				name: "cilium-vsphere",
				dir:  "example/rke2-cilium-vsphere",
				imageLists: []exampleImageList{
					{asset: "rke2-images-cilium.linux-amd64.txt", charts: []string{"rke2-cilium"}},
					// This list mixes the two vSphere charts' images. The trailing colon
					// matters: without it the pattern also claims
					// mirrored-cloud-provider-vsphere-csi-release-driver, which is the CSI
					// driver rather than the cloud provider.
					{
						asset:  "rke2-images-vsphere.linux-amd64.txt",
						charts: []string{"rancher-vsphere-cpi", "rancher-vsphere-csi"},
						owns:   map[string][]string{"rancher-vsphere-cpi": {"mirrored-cloud-provider-vsphere:"}},
					},
				},
				replacesKubeProxy: true,
				cloudProvider:     "rancher-vsphere",
				values:            exampleRKE2Values,
				// amd64 only, and deliberately: this is the one flavor that cannot be
				// multi-architecture. rke2 publishes an arm64 twin of every image list it
				// ships -- canal, cilium, core, flannel, multus, ingress-nginx -- except
				// this one, because the three images that are actually the vSphere drivers
				// are not built for arm64 by anyone:
				//
				//	mirrored-cloud-provider-vsphere                single-platform
				//	mirrored-cloud-provider-vsphere-csi-release-driver   linux+windows/amd64
				//	mirrored-cloud-provider-vsphere-csi-release-syncer   single-platform
				//
				// It is not a Rancher mirroring gap. registry.k8s.io serves the same three
				// with the same platforms, so there is no other tag to point at. The six
				// generic sig-storage sidecars in the list do have arm64, which is what
				// makes this worth writing down: an arm64 render would resolve most of the
				// chart and fail only on the drivers, which reads like a transient registry
				// problem rather than a thing that cannot work.
				//
				// So do not add arches: exampleMultiArches here, and do not rename the
				// directory to rke2-multi-cilium-vsphere -- the name would promise an
				// architecture the charts have no images for. Recheck the three manifests
				// before reversing this; nothing watches them.
			},
			{
				cni:  "canal",
				name: "multi-cni-canal",
				dir:  "example/rke2-multi-cni-canal",
				// canal needs no chart manifest, so its images stay in the flat list.
				imageLists: []exampleImageList{{asset: "rke2-images-canal.linux-amd64.txt"}},
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
		fetch: fetchRKE2EtcdVersion,
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

// rke2HardenedEtcd matches the etcd version out of RKE2's hardened-etcd image reference, e.g.
// rancher/hardened-etcd:v3.5.21-k3s1 -> v3.5.21.
var rke2HardenedEtcd = regexp.MustCompile(`hardened-etcd:(v[0-9]+\.[0-9]+\.[0-9]+)`)

// fetchRKE2EtcdVersion takes the upstream etcd version bundled in the RKE2 release from the
// hardened-etcd image in its core image list, and builds the etcdctl download URLs per
// architecture.
//
// A release whose image list carries no hardened-etcd reference is an error rather than a
// fallback to some version pinned here: a pin in this file is one nobody bumps, and the etcdctl
// it named would be hashed and shipped as if it were the one this release runs.
func fetchRKE2EtcdVersion(v *exampleVersion, _ string, _ *exampleShasums) error {
	var etcdVer string
	for _, img := range v.CoreImages {
		if m := rke2HardenedEtcd.FindStringSubmatch(img); len(m) > 1 {
			etcdVer = m[1]
			break
		}
	}
	if etcdVer == "" {
		return fmt.Errorf("no hardened-etcd image in the core image list for %s", v.TagURL)
	}
	v.EtcdVersion = etcdVer

	for i := range v.Arches {
		a := &v.Arches[i]
		a.EtcdTarball = fmt.Sprintf("etcd-%s-linux-%s.tar.gz", etcdVer, a.Arch)
		a.EtcdURL = fmt.Sprintf("https://github.com/etcd-io/etcd/releases/download/%s/%s",
			etcdVer, a.EtcdTarball)
	}
	return nil
}

// exampleK3sBinary is the name a k3s release publishes its binary for an architecture under.
// amd64 is the unsuffixed one, every other architecture carries its own name.
func exampleK3sBinary(arch string) string {
	if arch == "amd64" {
		return "k3s"
	}
	return "k3s-" + arch
}

// fetchK3sBinarySHA resolves each architecture's k3s binary digest, from example/shasums.json
// when it is already recorded there and otherwise out of the release's own checksum file for
// that architecture. Every k3s release publishes one, so the binaries themselves -- tens of
// megabytes each -- never have to be downloaded to be verified.
//
// The entry is keyed by archiveKey rather than by file name: a release publishes its binary as
// plain "k3s" (or "k3s-arm64"), so a file-name key would be the same for every version and each
// one would evict the last. archiveKey puts the release tag above it -- v1.35.0%2Bk3s1/k3s --
// which is the same reason an archive member's key carries one. This is what keeps the digests
// in the one file that tracks digests, rather than caching whole checksum assets beside the
// image manifests in example/assets.json.
func fetchK3sBinarySHA(v *exampleVersion, repoURL string, sums *exampleShasums) error {
	for i := range v.Arches {
		a := &v.Arches[i]
		key := archiveKey(a.BinaryURL)
		if e, ok := sums.lookupKey(key, a.BinaryURL); ok {
			a.BinarySHA = e.SHA256
			continue
		}

		asset := fmt.Sprintf("sha256sum-%s.txt", a.Arch)
		lines, err := fetchReleaseLines(repoURL, v.TagURL, asset)
		if err != nil {
			return err
		}
		sum, err := k3sBinarySHA(lines, exampleK3sBinary(a.Arch))
		if err != nil {
			return fmt.Errorf("%s for %s: %w", asset, v.TagURL, err)
		}
		a.BinarySHA = sum
		sums.storeKey(key, a.BinaryURL, sum)
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
