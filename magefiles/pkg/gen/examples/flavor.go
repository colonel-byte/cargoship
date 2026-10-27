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

// This file holds what a flavor is: one build of a distro an example is rendered for, and the
// tag filtering that follows from the minor lines it names. The distros the flavors belong to
// are declared in distros.go.

package examples

import (
	"slices"
	"strings"

	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/engineconfig"
)

// exampleFlavor is one build of a distro the template renders. Flavors are usually named for
// their CNI and written to their own directory, since the CNI choice reaches further than the
// image list: cilium replaces kube-proxy and is configured through a HelmChartConfig
// manifest, while canal and flannel run alongside kube-proxy and need no manifest. The
// multi-architecture flavors are the exception: they are named for what they demonstrate
// rather than for a CNI, since the CNI is not what makes them worth having.
type exampleFlavor struct {
	cni               string             // cilium -- what the template configures
	name              string             // multi -- what follows the distro in metadata.name; the CNI when empty
	dir               string             // example/rke2-cilium -- where its examples are written
	imageLists        []exampleImageList // the airgap manifests it adds to the core one
	replacesKubeProxy bool               // whether the CNI takes over from kube-proxy
	values            []string           // the values templates written next to distro.yaml; none when empty
	cloudProvider     string             // rancher-vsphere -- the bundled cloud provider it selects; none when empty
	arches            []string           // the architectures its examples target; amd64 alone when empty
	minors            []string           // v1_36 -- the minor lines it renders, all of them when empty
}

// exampleImageList is one of upstream's airgap image manifests, and the chart entries that
// declare the images in it.
//
// Upstream does not divide these lists along chart lines: rke2's vSphere manifest holds the
// CPI image beside the CSI driver and syncer, and hardened-cni-plugins is in both the cilium
// and the vSphere manifests. So a list several charts draw from is declared under each of
// them. The package carries each image once either way, and an image reaches a host while any
// chart declaring it is enabled -- which is what keeps a cluster that turns the CSI chart off
// from losing the CPI image it still runs. The dependency only runs one way: vSphere CSI needs
// the ProviderID the CPI sets, so CPI without CSI is ordinary and the reverse does not work.
type exampleImageList struct {
	// asset is the manifest's file name, e.g. rke2-images-cilium.linux-amd64.txt.
	asset string
	// charts names the chart entries that declare these images, residual owner last. Empty
	// means they belong to the flat imageConfig.images list, which is where a CNI with no
	// chart manifest keeps them.
	charts []string
	// owns maps a chart to the reference substrings that identify its images. An image is
	// declared by every chart whose pattern matches it, so one both charts need is not
	// withheld by whichever happened to be listed first. An image no pattern matches goes
	// to the residual owner -- the last chart in charts -- which is what keeps an image
	// upstream adds to a list from being dropped instead of merely misfiled.
	//
	// A chart whose images are the bulk of a list is therefore better left unnamed. rke2's
	// vSphere list is one CPI image and eight CSI ones, so CPI is named and CSI is residual:
	// the next storage sidecar upstream adds lands with the driver without an edit here.
	owns map[string][]string
}

// imageCharts returns the chart entries that declare image, out of the charts this list feeds.
func (l exampleImageList) imageCharts(image string) []string {
	var named []string
	for _, chart := range l.charts {
		for _, pattern := range l.owns[chart] {
			if strings.Contains(image, pattern) {
				named = append(named, chart)
				break
			}
		}
	}
	if len(named) > 0 {
		return named
	}
	return l.charts[len(l.charts)-1:]
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
	minor, err := engineconfig.TagMinor(tag)
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
