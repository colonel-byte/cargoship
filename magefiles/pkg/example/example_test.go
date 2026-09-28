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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilterFlavorTagsKeepsEverythingWhenAFlavorNamesNoMinors(t *testing.T) {
	tags := []string{"v1.36.4+rke2r1", "v1.32.9+rke2r1"}

	got, err := filterFlavorTags(tags, flavor{cni: "cilium"})
	require.NoError(t, err)
	require.Equal(t, tags, got)
}

func TestFilterFlavorTagsKeepsOnlyTheMinorsAFlavorCovers(t *testing.T) {
	f := flavor{cni: "cilium", name: "multi", minors: []string{"v1_35", "v1_36"}}

	got, err := filterFlavorTags([]string{
		"v1.36.4+rke2r1",
		"v1.35.8+rke2r1",
		"v1.34.1+rke2r1",
	}, f)
	require.NoError(t, err)
	require.Equal(t, []string{"v1.36.4+rke2r1", "v1.35.8+rke2r1"}, got)
}

// A tag that is not a version cannot be placed on a minor line, and a flavor that renders
// every line never has to ask -- so only a flavor with minors reports it.
func TestFilterFlavorTagsRejectsATagWithNoVersion(t *testing.T) {
	_, err := filterFlavorTags([]string{"latest"}, flavor{minors: []string{"v1_36"}})
	require.Error(t, err)

	got, err := filterFlavorTags([]string{"latest"}, flavor{})
	require.NoError(t, err)
	require.Equal(t, []string{"latest"}, got)
}

func TestFlavorNameFallsBackToTheCNI(t *testing.T) {
	require.Equal(t, "multi", flavor{cni: "cilium", name: "multi"}.flavorName())
	require.Equal(t, "cilium", flavor{cni: "cilium"}.flavorName())
}

func TestFlavorArchitecturesDefaultToAMD64(t *testing.T) {
	require.Equal(t, []string{"amd64"}, flavor{}.architectures())
	require.Equal(t, []string{"amd64", "arm64"}, flavor{arches: []string{"amd64", "arm64"}}.architectures())
}

func TestRKE2RPMURL(t *testing.T) {
	require.Equal(t,
		"https://rpm.rancher.io/rke2/latest/v1.36/centos/9/x86_64/rke2-server-1.36.4~rke2r1-0.el9.x86_64.rpm",
		rke2RPM("server", "v1.36", "1.36.4~rke2r1", "x86_64"))
	require.Equal(t,
		"https://rpm.rancher.io/rke2/latest/v1.36/centos/9/aarch64/rke2-agent-1.36.4~rke2r1-0.el9.aarch64.rpm",
		rke2RPM("agent", "v1.36", "1.36.4~rke2r1", "aarch64"))
}

// amd64 is the unsuffixed binary upstream publishes, every other architecture carries its own
// suffix -- which is also what tells the two apart in the checksum file.
func TestK3sBinaryName(t *testing.T) {
	require.Equal(t, "k3s", k3sBinary("amd64"))
	require.Equal(t, "k3s-arm64", k3sBinary("arm64"))
}

func TestK3sBinarySHAPicksTheNamedBinary(t *testing.T) {
	lines := []string{
		"aaaa  k3s-airgap-images-amd64.tar.zst",
		"bbbb  k3s",
		"cccc  k3s-arm64",
	}

	sum, err := k3sBinarySHA(lines, "k3s")
	require.NoError(t, err)
	require.Equal(t, "bbbb", sum)

	sum, err = k3sBinarySHA(lines, "k3s-arm64")
	require.NoError(t, err)
	require.Equal(t, "cccc", sum)
}

func TestK3sBinarySHAReportsAMissingEntry(t *testing.T) {
	_, err := k3sBinarySHA([]string{"aaaa  k3s"}, "k3s-arm64")
	require.ErrorContains(t, err, "k3s-arm64")
}

// Every flavor renders into its own directory, and the name a flavor contributes to an
// example's metadata.name has to be unique too, or two flavors of one distro would write the
// same example over each other.
func TestDistroFlavorsAreDistinct(t *testing.T) {
	for _, spec := range distros {
		dirs := map[string]bool{}
		names := map[string]bool{}
		for _, f := range spec.flavors {
			require.NotEmpty(t, f.dir, "%s flavor %s has no directory", spec.name, f.flavorName())
			require.False(t, dirs[f.dir], "%s renders two flavors into %s", spec.name, f.dir)
			require.False(t, names[f.flavorName()], "%s has two %s flavors", spec.name, f.flavorName())
			dirs[f.dir] = true
			names[f.flavorName()] = true
		}
	}
}

func TestDistroByNameListsTheNamesThatWork(t *testing.T) {
	spec, err := distroByName("rke2")
	require.NoError(t, err)
	require.Equal(t, "rke2", spec.name)

	_, err = distroByName("k8s")
	require.ErrorContains(t, err, "rke2")
}
