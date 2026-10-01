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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExampleFlavorName(t *testing.T) {
	f1 := exampleFlavor{
		cni:  "cilium",
		name: "multi-cni-cilium",
	}
	assert.Equal(t, "multi-cni-cilium", f1.flavorName())

	f2 := exampleFlavor{
		cni: "flannel",
	}
	assert.Equal(t, "flannel", f2.flavorName())
}

func TestExampleFlavorArchitectures(t *testing.T) {
	fDefault := exampleFlavor{}
	assert.Equal(t, []string{"amd64"}, fDefault.architectures())

	fCustom := exampleFlavor{
		arches: []string{"amd64", "arm64"},
	}
	assert.Equal(t, []string{"amd64", "arm64"}, fCustom.architectures())
}

func TestExampleFlavorCoversAndFilter(t *testing.T) {
	fAll := exampleFlavor{}
	covers, err := fAll.covers("v1.35.2+k3s1")
	require.NoError(t, err)
	assert.True(t, covers)

	fSpecific := exampleFlavor{
		minors: []string{"v1_35", "v1_36"},
	}
	covers, err = fSpecific.covers("v1.35.2+k3s1")
	require.NoError(t, err)
	assert.True(t, covers)

	covers, err = fSpecific.covers("v1.34.1+k3s1")
	require.NoError(t, err)
	assert.False(t, covers)

	tags := []string{
		"v1.34.1+k3s1",
		"v1.35.2+k3s1",
		"v1.36.0+k3s1",
		"v1.37.0+k3s1",
	}

	filtered, err := filterFlavorTags(tags, fSpecific)
	require.NoError(t, err)
	assert.Equal(t, []string{"v1.35.2+k3s1", "v1.36.0+k3s1"}, filtered)
}

func TestSortTagsDesc(t *testing.T) {
	tags := []string{
		"v1.34.1+k3s1",
		"v1.36.1+k3s1",
		"v1.35.2+k3s1",
		"v1.36.2+k3s1",
	}

	err := sortTagsDesc(tags)
	require.NoError(t, err)

	expected := []string{
		"v1.36.2+k3s1",
		"v1.36.1+k3s1",
		"v1.35.2+k3s1",
		"v1.34.1+k3s1",
	}
	assert.Equal(t, expected, tags)
}

func TestExampleDistroByName(t *testing.T) {
	spec, err := exampleDistroByName("rke2")
	require.NoError(t, err)
	assert.Equal(t, "rke2", spec.name)

	spec, err = exampleDistroByName("k3s")
	require.NoError(t, err)
	assert.Equal(t, "k3s", spec.name)

	_, err = exampleDistroByName("invalid")
	assert.Error(t, err)
}
