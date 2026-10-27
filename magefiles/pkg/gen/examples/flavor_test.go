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

package examples

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// vsphereImageList is the routing rke2's vSphere airgap manifest gets, copied from the flavor
// in distros.go so a change to one is a failure here rather than a silently different split.
var vsphereImageList = exampleImageList{
	asset:  "rke2-images-vsphere.linux-amd64.txt",
	charts: []string{"rancher-vsphere-cpi", "rancher-vsphere-csi"},
	owns:   map[string][]string{"rancher-vsphere-cpi": {"mirrored-cloud-provider-vsphere:"}},
}

func TestImageChartsRoutesTheVsphereList(t *testing.T) {
	for _, tt := range []struct {
		name  string
		image string
		want  []string
	}{
		{
			name:  "the cloud provider is the CPI chart's",
			image: "docker.io/rancher/mirrored-cloud-provider-vsphere:v1.37.0",
			want:  []string{"rancher-vsphere-cpi"},
		},
		{
			// The pattern's trailing colon is what keeps this out of the CPI chart: the
			// driver's reference starts with the cloud provider's name.
			name:  "the CSI driver is not the cloud provider",
			image: "docker.io/rancher/mirrored-cloud-provider-vsphere-csi-release-driver:v3.7.3",
			want:  []string{"rancher-vsphere-csi"},
		},
		{
			name:  "the CSI syncer is not the cloud provider either",
			image: "docker.io/rancher/mirrored-cloud-provider-vsphere-csi-release-syncer:v3.7.3",
			want:  []string{"rancher-vsphere-csi"},
		},
		{
			name:  "a storage sidecar falls to the residual owner",
			image: "docker.io/rancher/mirrored-sig-storage-csi-provisioner:v4.0.1",
			want:  []string{"rancher-vsphere-csi"},
		},
		{
			// The case the residual exists for: an image upstream adds to the list that no
			// pattern anticipated lands with the driver rather than being dropped.
			name:  "an image nobody anticipated falls to the residual owner",
			image: "docker.io/rancher/mirrored-sig-storage-csi-something-new:v1.0.0",
			want:  []string{"rancher-vsphere-csi"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, vsphereImageList.imageCharts(tt.image))
		})
	}
}

// Every version of the vSphere list seen so far holds one CPI image and the rest CSI, across
// three minor lines and four cloud-provider releases. The references differ only in their
// tags, so routing the whole list is what proves the split rather than any single entry.
func TestImageChartsSplitsEveryVsphereRelease(t *testing.T) {
	for _, cpiTag := range []string{"v1.35.0", "v1.35.1", "v1.36.0", "v1.37.0"} {
		t.Run(cpiTag, func(t *testing.T) {
			list := []string{
				"docker.io/rancher/mirrored-cloud-provider-vsphere:" + cpiTag,
				"docker.io/rancher/mirrored-cloud-provider-vsphere-csi-release-driver:v3.7.3",
				"docker.io/rancher/mirrored-cloud-provider-vsphere-csi-release-syncer:v3.7.3",
				"docker.io/rancher/mirrored-sig-storage-csi-node-driver-registrar:v2.13.0",
				"docker.io/rancher/mirrored-sig-storage-csi-resizer:v1.12.0",
				"docker.io/rancher/mirrored-sig-storage-livenessprobe:v2.15.0",
				"docker.io/rancher/mirrored-sig-storage-csi-attacher:v4.9.0",
				"docker.io/rancher/mirrored-sig-storage-csi-provisioner:v4.0.1",
				"docker.io/rancher/hardened-csi-snapshotter:v8.6.0-build20260909",
			}

			got := map[string][]string{}
			for _, image := range list {
				for _, chart := range vsphereImageList.imageCharts(image) {
					got[chart] = append(got[chart], image)
				}
			}

			require.Len(t, got["rancher-vsphere-cpi"], 1, "the cloud provider image, and only it")
			require.Len(t, got["rancher-vsphere-csi"], 8, "the driver and its sidecars")
		})
	}
}

// A list with one chart needs no patterns: that chart is the residual owner, so it takes
// everything. This is how every CNI list is declared.
func TestImageChartsWithASingleChart(t *testing.T) {
	list := exampleImageList{
		asset:  "rke2-images-cilium.linux-amd64.txt",
		charts: []string{"rke2-cilium"},
	}

	require.Equal(t, []string{"rke2-cilium"}, list.imageCharts("docker.io/rancher/mirrored-cilium-cilium:v1.20.2"))
	require.Equal(t, []string{"rke2-cilium"}, list.imageCharts("docker.io/rancher/anything-at-all:1"))
}

// An image several charts claim is declared by all of them, so that disabling one chart does
// not withhold it from another that still needs it.
func TestImageChartsSharedBetweenCharts(t *testing.T) {
	list := exampleImageList{
		asset:  "shared.txt",
		charts: []string{"chart-a", "chart-b"},
		owns: map[string][]string{
			"chart-a": {"shared-", "only-a"},
			"chart-b": {"shared-"},
		},
	}

	require.Equal(t, []string{"chart-a", "chart-b"}, list.imageCharts("example.com/shared-thing:1"))
	require.Equal(t, []string{"chart-a"}, list.imageCharts("example.com/only-a:1"))
	require.Equal(t, []string{"chart-b"}, list.imageCharts("example.com/unclaimed:1"), "residual")
}
