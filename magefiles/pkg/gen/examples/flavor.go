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

	"github.com/colonel-byte/cargoship/magefiles/pkg/gen/engineconfig"
)

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
	values            []string // the values templates written next to distro.yaml; none when empty
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
