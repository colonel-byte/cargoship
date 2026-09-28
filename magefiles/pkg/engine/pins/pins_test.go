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

package pins

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/colonel-byte/cargoship/magefiles/pkg/repo"
	"github.com/stretchr/testify/require"
)

func TestTagVersion(t *testing.T) {
	for _, tt := range []struct {
		tag  string
		want [3]int
	}{
		{"v1.35.8+k3s1", [3]int{1, 35, 8}},
		{"v1.36.1+rke2r2", [3]int{1, 36, 1}},
		// No suffix at all, and a double-digit patch, which naive string ordering gets wrong.
		{"v1.32.13", [3]int{1, 32, 13}},
		// Only the leading triple is read, so a pre-release suffix parses like any other.
		{"v1.37.0-rc1+k3s1", [3]int{1, 37, 0}},
	} {
		t.Run(tt.tag, func(t *testing.T) {
			got, err := TagVersion(tt.tag)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestTagVersionRejectsWhatItCannotParse(t *testing.T) {
	for _, tag := range []string{"", "1.35.8", "v1.35", "release-1.35.8", "vX.Y.Z"} {
		t.Run(tag, func(t *testing.T) {
			_, err := TagVersion(tag)
			require.ErrorContains(t, err, "does not start with vMAJOR.MINOR.PATCH")
		})
	}
}

func TestTagMinor(t *testing.T) {
	// The underscore is what makes it a legal Go package name, which is the whole reason the
	// directory is not named after the tag.
	got, err := TagMinor("v1.35.8+k3s1")
	require.NoError(t, err)
	require.Equal(t, "v1_35", got)

	_, err = TagMinor("nonsense")
	require.Error(t, err)
}

func TestSortTags(t *testing.T) {
	d := &Distro{Tags: []string{
		"v1.33.13+k3s1", "v1.37.0+k3s1", "v1.9.1+k3s1", "v1.35.8+k3s1", "v1.10.0+k3s1",
	}}

	require.NoError(t, d.SortTags())

	// Newest first, and v1.10 above v1.9 -- the ordering a string sort inverts.
	require.Equal(t, []string{
		"v1.37.0+k3s1", "v1.35.8+k3s1", "v1.33.13+k3s1", "v1.10.0+k3s1", "v1.9.1+k3s1",
	}, d.Tags)
}

func TestSortTagsReportsAnUnparseableTag(t *testing.T) {
	d := &Distro{Tags: []string{"v1.35.8+k3s1", "latest", "v1.33.13+k3s1"}}

	require.Error(t, d.SortTags())
}

// TestSetTagReplacesItsOwnMinorLine is the case that makes pins.json a manifest of minor lines
// rather than a list of tags: pinning a new patch must overwrite the line it belongs to and
// leave every other line alone.
func TestSetTagReplacesItsOwnMinorLine(t *testing.T) {
	d := &Distro{Name: "k3s", Tags: []string{"v1.36.4+k3s1", "v1.35.8+k3s1", "v1.34.11+k3s1"}}

	prev, changed, err := d.SetTag("v1.35.9+k3s1")
	require.NoError(t, err)
	require.Equal(t, "v1.35.8+k3s1", prev)
	require.True(t, changed)
	require.Equal(t, []string{"v1.36.4+k3s1", "v1.35.9+k3s1", "v1.34.11+k3s1"}, d.Tags)
}

// TestSetTagAddsANewMinorLineInOrder covers the other half: a line nobody has pinned before is
// appended and then sorted into place, so it does not land at the bottom of the file.
func TestSetTagAddsANewMinorLineInOrder(t *testing.T) {
	d := &Distro{Name: "k3s", Tags: []string{"v1.36.4+k3s1", "v1.34.11+k3s1"}}

	prev, changed, err := d.SetTag("v1.35.8+k3s1")
	require.NoError(t, err)
	require.Empty(t, prev, "a new minor line replaced nothing")
	require.True(t, changed)
	require.Equal(t, []string{"v1.36.4+k3s1", "v1.35.8+k3s1", "v1.34.11+k3s1"}, d.Tags)
}

// TestSetTagReportsNoChange is what keeps a routine update run from rewriting pins.json and
// re-pulling source it already has.
func TestSetTagReportsNoChange(t *testing.T) {
	d := &Distro{Name: "k3s", Tags: []string{"v1.36.4+k3s1", "v1.35.8+k3s1"}}

	prev, changed, err := d.SetTag("v1.35.8+k3s1")
	require.NoError(t, err)
	require.Equal(t, "v1.35.8+k3s1", prev)
	require.False(t, changed)
	require.Equal(t, []string{"v1.36.4+k3s1", "v1.35.8+k3s1"}, d.Tags)
}

func TestSetTagRejectsAnUnparseableTag(t *testing.T) {
	d := &Distro{Name: "k3s", Tags: []string{"v1.35.8+k3s1"}}

	_, _, err := d.SetTag("latest")
	require.Error(t, err)
	require.Equal(t, []string{"v1.35.8+k3s1"}, d.Tags, "a rejected tag changes nothing")
}

func TestDistroNamesWhatItKnows(t *testing.T) {
	p := &Pins{Distros: []Distro{{Name: "k3s"}, {Name: "rke2"}}}

	d, err := p.Distro("rke2")
	require.NoError(t, err)
	require.Equal(t, "rke2", d.Name)

	// The error is the one a mistyped `mage generate:latestTag` argument produces, so it lists
	// the alternatives rather than only rejecting the input.
	_, err = p.Distro("k8s")
	require.ErrorContains(t, err, `unknown distro "k8s"`)
	require.ErrorContains(t, err, "k3s, rke2")
}

func TestPullsFlattensEveryPinnedTag(t *testing.T) {
	p := Pins{Distros: []Distro{
		{
			Name:  "k3s",
			Repo:  "https://github.com/k3s-io/k3s",
			Files: []string{"pkg/cli/cmds/server.go"},
			Tags:  []string{"v1.36.4+k3s1", "v1.35.8+k3s1"},
		},
		{Name: "rke2", Repo: "https://github.com/rancher/rke2", Tags: []string{"v1.36.1+rke2r1"}},
	}}

	pulls, err := p.Pulls()
	require.NoError(t, err)
	require.Len(t, pulls, 3)

	require.Equal(t, Pull{
		RepoURL: "https://github.com/k3s-io/k3s",
		Tag:     "v1.36.4+k3s1",
		DestDir: filepath.Join(Dir, "k3s", "v1_36"),
		Files:   []string{"pkg/cli/cmds/server.go"},
	}, pulls[0])
	require.Equal(t, filepath.Join(Dir, "rke2", "v1_36"), pulls[2].DestDir)
}

func TestPullsReportsAnUnparseableTag(t *testing.T) {
	p := Pins{Distros: []Distro{{Name: "k3s", Tags: []string{"latest"}}}}

	_, err := p.Pulls()
	require.ErrorContains(t, err, "k3s")
}

// TestReadWriteRoundTrip checks the manifest survives a rewrite byte for byte, which is what
// lets `mage generate:updatePins` leave an untouched line untouched in the diff.
func TestReadWriteRoundTrip(t *testing.T) {
	repo.Chdir(t)

	before, err := os.ReadFile(Path)
	require.NoError(t, err)

	manifest, err := Read()
	require.NoError(t, err)
	require.NotEmpty(t, manifest.Distros)

	// Written into a copy of the tree, so a failure here cannot dirty the checkout.
	t.Chdir(t.TempDir())
	require.NoError(t, os.MkdirAll(Dir, 0o755))
	require.NoError(t, manifest.Write())

	after, err := os.ReadFile(Path)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after))
}

// TestCheckedInTagsAreSortedAndOnDistinctLines holds the two invariants SetTag maintains, so a
// hand edit to pins.json that breaks either fails here rather than on the next update run.
func TestCheckedInTagsAreSortedAndOnDistinctLines(t *testing.T) {
	repo.Chdir(t)

	manifest, err := Read()
	require.NoError(t, err)

	for _, d := range manifest.Distros {
		t.Run(d.Name, func(t *testing.T) {
			require.NotEmpty(t, d.Tags)

			sorted := &Distro{Tags: append([]string(nil), d.Tags...)}
			require.NoError(t, sorted.SortTags())
			require.Equal(t, sorted.Tags, d.Tags, "tags are newest first")

			seen := map[string]string{}
			for _, tag := range d.Tags {
				minor, err := TagMinor(tag)
				require.NoError(t, err)
				require.NotContains(t, seen, minor, "one tag per minor line")
				seen[minor] = tag
			}
		})
	}
}
