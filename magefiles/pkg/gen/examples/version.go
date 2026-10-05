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

// This file holds what the example templates render against -- one tag plus one flavor
// resolved into every field a distro.yaml needs -- and the derivation that gets it there.

package examples

import (
	"fmt"
	"strings"

	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/engineconfig"
	"github.com/colonel-byte/cargoship/pkg/engineconfig/gen"
)

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

	// RKE2 installs from RPMs, plus a release tarball its binaries, scripts and unit files
	// are extracted from. The tarball is named and addressed here rather than in the
	// templates so that the URL hashed is the same one the example carries.
	CommonRPM  string
	ServerRPM  string
	AgentRPM   string
	Tarball    string // rke2.linux-amd64.tar.gz
	TarballURL string // https://github.com/rancher/rke2/releases/download/<tag>/<tarball>

	// k3s installs a single binary, whose digest the release publishes for us.
	BinaryURL string
	BinarySHA string
}

// newExampleVersion derives every version-varying field of an example from one tag and the
// flavor being rendered, except what has to be fetched -- the image lists, and whatever the
// distro's own fetch hook adds.
func newExampleVersion(tag string, spec exampleDistroSpec, f exampleFlavor) (exampleVersion, error) {
	semver, err := engineconfig.TagVersion(tag)
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

	// Missing only for a version whose source was never pulled into this build, which
	// writeExampleValues turns into an error rather than a schema that accepts nothing.
	if entry, ok := gen.Lookup(spec.name, v.Version); ok {
		v.Addons = entry.Addons
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
