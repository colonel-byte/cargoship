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

package layout

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// write creates the example directories a rendered tree would have, so Tags has something to
// read back.
func write(t *testing.T, dir string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, p), 0o755))
	}
}

func TestTagsReturnsThePinsWhenNothingIsOnDisk(t *testing.T) {
	got, err := Tags(filepath.Join(t.TempDir(), "absent"), "rke2", []string{"v1.35.8+rke2r1", "v1.36.4+rke2r1"})
	require.NoError(t, err)
	require.Equal(t, []string{"v1.36.4+rke2r1", "v1.35.8+rke2r1"}, got)
}

// An example already rendered is re-rendered whether or not its tag is still pinned, so a
// template edit reaches it rather than leaving it to drift.
func TestTagsUnionsThePinsWithWhatIsOnDisk(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "v1_35/v1.35.8-rke2r1", "v1_34/v1.34.1-rke2r1")

	got, err := Tags(dir, "rke2", []string{"v1.36.4+rke2r1", "v1.35.8+rke2r1"})
	require.NoError(t, err)
	require.Equal(t, []string{"v1.36.4+rke2r1", "v1.35.8+rke2r1", "v1.34.1+rke2r1"}, got)
}

// A directory is named for its tag with the "+" written as a "-", so reading one back has to
// put the "+" of the distro separator -- and only that one -- in again.
func TestTagsRestoresTheDistroSeparator(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "v1_35/v1.35.8-k3s1")

	got, err := Tags(dir, "k3s", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"v1.35.8+k3s1"}, got)
}

// Upstream tags carry no "+" at all, so nothing is swapped back.
func TestTagsLeavesAnUpstreamTagAlone(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "v1_37/v1.37.0")

	got, err := Tags(dir, "upstream", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"v1.37.0"}, got)
}

func TestTagsSkipsWhatIsNotAnExample(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "v1_35/v1.35.8-rke2r1", "v1_35/notes", "shared", "README.md")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "v1_35", "distro.yaml"), nil, 0o644))

	got, err := Tags(dir, "rke2", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"v1.35.8+rke2r1"}, got)
}

func TestTagsDoesNotRepeatATagThatIsPinnedAndRendered(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "v1_36/v1.36.4-rke2r1")

	got, err := Tags(dir, "rke2", []string{"v1.36.4+rke2r1"})
	require.NoError(t, err)
	require.Equal(t, []string{"v1.36.4+rke2r1"}, got)
}
