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
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/colonel-byte/cargoship/pkg/coci"
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

// exampleChartMinors gates the chart flavors to the newest line.
//
// They demonstrate where a chart comes from, which has nothing to do with the Kubernetes
// version: the section a chart is declared in, and the resource cargoship writes for it, are the
// same on every line. Rendering them for every line would commit sixty directories differing
// only in the engine version beside them.
var exampleChartMinors = []string{"v1_37"}

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

	// haulerTemplates and zarfTemplates are the content-collection manifest templates
	// rendered for a flavor that asks for that format -- the two a build can read back
	// through --content-store. Per distro rather than per flavor because what a release
	// publishes to install from is a property of the distro: rke2 has RPMs and a tarball,
	// k3s has one binary.
	//
	// They are two lists rather than one because the formats are chosen per flavor and not
	// together: an operator who standardised on one has no use for the other, and generating
	// both everywhere doubles what a reviewer reads for examples nobody collects that way.
	haulerTemplates []string
	zarfTemplates   []string

	// kubeVip is the control-plane VIP block a flavor of this distro gets when it asks for
	// one. Per distro because the two fields that are not kube-vip's own follow from the
	// engine: where its kubelet reads static pods, and where it writes the admin kubeconfig
	// the pod holds its lease with. Nil for a distro no kube-vip flavor is declared for.
	kubeVip *exampleKubeVip

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
	// exampleRKE2Hauler and exampleRKE2Zarf are the content-collection manifests an rke2
	// flavor renders when it asks for that format, one template per output. Each guards its
	// own body, so a template describing something a flavor does not have writes no file --
	// see writeExampleContentManifests, which also explains why the zarf definitions split
	// only by architecture while the hauler manifests split further.
	exampleRKE2Hauler = []string{
		"magefiles/templates/rke2/hauler-images-core.yaml.tmpl",
		"magefiles/templates/rke2/hauler-images-rke2-cilium.yaml.tmpl",
		"magefiles/templates/rke2/hauler-files-amd64.yaml.tmpl",
		"magefiles/templates/rke2/hauler-files-arm64.yaml.tmpl",
	}
	exampleRKE2Zarf = []string{
		"magefiles/templates/rke2/zarf-amd64.yaml.tmpl",
		"magefiles/templates/rke2/zarf-arm64.yaml.tmpl",
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
		// Each rendered only for a flavor that asks for that format; see
		// writeExampleContentManifests.
		haulerTemplates: exampleRKE2Hauler,
		zarfTemplates:   exampleRKE2Zarf,
		// rke2 serves the API on 6443 and the supervisor joining nodes use on 9345. kube-vip
		// health-checks the API port; in ARP mode it holds the address rather than
		// forwarding a port, so the supervisor is reachable on the VIP either way.
		kubeVip: newExampleKubeVip("/var/lib/rancher/rke2", "/etc/rancher/rke2/rke2.yaml", "6443"),
		flavors: []exampleFlavor{
			{
				cni:               "cilium",
				name:              "multi-cni-cilium",
				dir:               "example/rke2-multi-cni-cilium",
				imageLists:        []exampleImageList{{asset: "rke2-images-cilium.linux-amd64.txt", charts: []string{"rke2-cilium"}}},
				arches:            exampleMultiArches,
				haulerManifests:   true,
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
				// The zarf definitions go here rather than on a multi-architecture flavor,
				// and the arm64 note below is why: a zarf package carries a single
				// metadata.architecture, so a flavor targeting two architectures needs two
				// packages and `--content-store` takes one path. This flavor is the one that
				// structurally cannot want a second, so its generated definition is complete
				// as it stands. See docs/guides/content-store.md.
				//
				// It is also the flavor that asks for no hauler manifests, which makes it the
				// proof that the two formats are gated independently.
				zarfPackages: true,
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
				// A control-plane VIP, held by kube-vip on whichever controller wins its
				// lease. It is its own flavor rather than a value on the others because
				// the image has to be carried whether or not a given cluster turns the
				// pod on: spec.config.imageConfig.images has no per-value gate, so adding
				// kube-vip to an existing flavor would upload its image to every host of
				// every cluster that installs one.
				//
				// canal rather than cilium, and amd64 alone, to keep the flavor about the
				// one thing it demonstrates.
				cni:     "canal",
				name:    "kube-vip",
				dir:     "example/rke2-kube-vip",
				kubeVip: true,
				// canal needs no chart manifest, so its images stay in the flat list.
				imageLists: []exampleImageList{{asset: "rke2-images-canal.linux-amd64.txt"}},
				values:     exampleRKE2Values,
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
		fetch: fetchRKE2Release,
		// RKE2 installs from RPMs, and Rancher removes an rke2rN's RPMs once the next
		// revision supersedes it, long before the git tag goes anywhere. Every architecture
		// of a build is published and pulled together, so the first one answers for all.
		probe: func(v exampleVersion) string { return v.Arches[0].CommonRPM },
	},
	{
		name:       "k3s",
		template:   "magefiles/templates/k3s-distro.yaml.tmpl",
		coreImages: "k3s-images.txt",
		// Same paths as rke2 one directory over, and the same two ports.
		kubeVip: newExampleKubeVip("/var/lib/rancher/k3s", "/etc/rancher/k3s/k3s.yaml", "6443"),
		// k3s ships its CNI in the binary, so flannel is what a stock k3s runs, and its
		// images are already in k3s-images.txt.
		flavors: []exampleFlavor{
			{cni: "flannel", dir: "example/k3s-flannel", values: exampleK3sValues},
			{
				// The control-plane VIP flavor; see its rke2 twin for why it is a flavor
				// of its own rather than a value on the others.
				cni:     "flannel",
				name:    "kube-vip",
				dir:     "example/k3s-kube-vip",
				kubeVip: true,
				values:  exampleK3sValues,
			},
			{
				// A package that carries a Helm chart and deploys it through the helm
				// controller, downloaded and verified against a digest.
				//
				// k3s rather than rke2 for both chart flavors: the writer is
				// RancherCommon's, so rke2 installs these identically, and a twin flavor
				// per distro would render twenty more directories apiece to demonstrate
				// the same mechanism. Two flavors rather than one because the chart's
				// *source* is the part that differs in practice.
				cni:    "flannel",
				name:   "chart-http",
				dir:    "example/k3s-chart-http",
				chart:  chartSourceHTTP,
				values: exampleK3sValues,
				minors: exampleChartMinors,
			},
			{
				// A chart pulled from a registry as an OCI artifact, pinned to the digest
				// its tag resolved to -- which is why its entry carries no shasum.
				cni:    "flannel",
				name:   "chart-oci",
				dir:    "example/k3s-chart-oci",
				chart:  chartSourceOCI,
				values: exampleK3sValues,
				minors: exampleChartMinors,
			},
			{
				cni:    "flannel",
				name:   "multi",
				dir:    "example/k3s-multi",
				arches: exampleMultiArches,
				minors: exampleMultiMinors,
				values: exampleK3sValues,
			},
			{
				// The same flavor, taking the binary out of the published k3s image
				// instead of the release download. The image is built FROM scratch, so
				// its one layer is the root filesystem the binary sits in.
				cni:       "flannel",
				name:      "oci",
				dir:       "example/k3s-oci",
				arches:    exampleMultiArches,
				minors:    exampleMultiMinors,
				values:    exampleK3sValues,
				binaryOCI: true,
			},
		},
		derive: func(v *exampleVersion) {
			v.SelinuxRPM = exampleK3sSelinuxRPM
			if v.BinaryOCI {
				// One reference for every architecture; the arch selector picks the
				// platform manifest out of it.
				v.BinaryOCIRef = exampleK3sImageRef(v.Version)
				return
			}
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

// fetchRKE2Release fills in everything only an rke2 release can answer: the bundled etcd
// version, and the digest of the release tarball.
func fetchRKE2Release(v *exampleVersion, repoURL string, sums *exampleShasums) error {
	if err := fetchRKE2EtcdVersion(v, repoURL, sums); err != nil {
		return err
	}
	return fetchRKE2TarballSHA(v, repoURL, sums)
}

// fetchRKE2TarballSHA records the release tarball's digest from the checksum file the release
// publishes, rather than leaving it to be arrived at by downloading the tarball.
//
// The digest is wanted by the zarf definitions, which declare the tarball as a file and hash it
// with the templates' sha256 function. That function downloads what it is given, and the rke2
// tarball is about 230MiB -- so rendering the twenty-five versions of the flavor that asks for
// zarf definitions moved something like six gigabytes per run, every run, to compute numbers
// upstream had already published. It is also what made the render fail outright: the release
// host answers that volume with 502s.
//
// Recording it here means the template's own call finds it in the cache and fetches nothing.
// fetchK3sBinarySHA does the same thing for the k3s binary, for the same reason.
//
// Only the whole tarball is covered. The files extracted out of it still need the archive
// streamed -- a checksum file cannot give a digest for a path inside an archive -- but those
// entries are keyed per version and so are cached once rather than re-fetched; see store.
func fetchRKE2TarballSHA(v *exampleVersion, repoURL string, sums *exampleShasums) error {
	for i := range v.Arches {
		a := &v.Arches[i]
		key := archiveKey(a.TarballURL)
		if _, ok := sums.lookupKey(key, a.TarballURL); ok {
			continue
		}

		asset := fmt.Sprintf("sha256sum-%s.txt", a.Arch)
		lines, err := fetchReleaseLines(repoURL, v.TagURL, asset)
		if err != nil {
			// A release whose checksum file is missing is not a failure: the template's
			// own sha256 call still downloads and hashes the tarball, which is what
			// happened before this existed. Anything else -- a timeout, a DNS failure,
			// one of the 502s this function exists to stop provoking -- is, because
			// continuing past it silently restores the 230MiB download per version.
			if !errors.Is(err, errReleaseAssetMissing) {
				return fmt.Errorf("%s for %s: %w", asset, v.TagURL, err)
			}
			fmt.Printf("warning: %s for %s: %v\n", asset, v.TagURL, err)
			continue
		}
		sum, err := releaseAssetSHA(lines, a.Tarball)
		if err != nil {
			// The checksum file is published but names no tarball, which is the same
			// not-published case as above rather than a transport failure.
			fmt.Printf("warning: %s for %s: %v\n", asset, v.TagURL, err)
			continue
		}
		sums.storeKey(key, a.TarballURL, sum)
	}
	return nil
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
	if v.BinaryOCI {
		return resolveK3sImage(v, sums)
	}
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
		sum, err := releaseAssetSHA(lines, exampleK3sBinary(a.Arch))
		if err != nil {
			return fmt.Errorf("%s for %s: %w", asset, v.TagURL, err)
		}
		a.BinarySHA = sum
		sums.storeKey(key, a.BinaryURL, sum)
	}
	return nil
}

// exampleK3sImage is the repository the k3s image is published to.
const exampleK3sImage = "oci://docker.io/rancher/k3s"

// exampleK3sImageRef is the image tag matching a release version. The release tag spells the
// build with a plus -- v1.37.2+k3s1 -- which a registry cannot carry, so the image uses a
// hyphen and the version field already holds that spelling.
func exampleK3sImageRef(version string) string {
	return fmt.Sprintf("%s:v%s", exampleK3sImage, version)
}

// resolveK3sImage pins the image reference to the digest its tag resolves to, so the rendered
// example names bytes rather than a tag that can be re-pushed.
//
// Only manifests are read: the index, and the platform manifest the architecture selects.
// Neither is large and the layer itself is never fetched, so rendering an example line costs
// no more than reading a release checksum file does.
func resolveK3sImage(v *exampleVersion, sums *exampleShasums) error {
	ref := exampleK3sImageRef(v.Version)
	if e, ok := sums.lookupKey(ref, ref); ok {
		v.BinaryOCIRef = exampleK3sPinned(ref, e.SHA256)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), exampleOCITimeout)
	defer cancel()

	// Any architecture the flavor renders resolves the same index digest; the first is as
	// good as another, and resolving one also proves the layer selection works.
	arch := "amd64"
	if len(v.Arches) > 0 {
		arch = v.Arches[0].Arch
	}
	resolution, err := coci.Resolve(ctx, ref, coci.FileSelector{Arch: arch}, coci.FileOptions{})
	if err != nil {
		return fmt.Errorf("resolving %s: %w", ref, err)
	}

	digest := strings.TrimPrefix(resolution.Reference[strings.LastIndex(resolution.Reference, "@")+1:], "sha256:")
	v.BinaryOCIRef = resolution.Reference
	sums.storeKey(ref, ref, digest)
	return nil
}

// exampleK3sPinned rebuilds a pinned reference from a cached digest.
func exampleK3sPinned(ref, digest string) string {
	return fmt.Sprintf("%s@sha256:%s", ref[:strings.LastIndex(ref, ":")], digest)
}

// exampleOCITimeout bounds the manifest reads a render makes, so a registry that is slow or
// unreachable fails the render rather than hanging it.
const exampleOCITimeout = 30 * time.Second

// releaseAssetSHA picks one asset's digest out of a release checksum file's lines. Both distros
// publish one of these, in the ordinary "<sha256>  <name>" form.
func releaseAssetSHA(lines []string, asset string) (string, error) {
	for _, line := range lines {
		// "<sha256>  k3s" or "<sha256>  rke2.linux-amd64.tar.gz", alongside the airgap tarballs.
		if sum, name, ok := strings.Cut(line, " "); ok && strings.TrimSpace(name) == asset {
			return sum, nil
		}
	}
	return "", fmt.Errorf("no %s entry", asset)
}
