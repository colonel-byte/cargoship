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

package phase

import (
	"testing"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1"
	"github.com/stretchr/testify/require"
)

// TestStagedFilesSelectsOnlyWhatStillHasToMove covers the decision behind the promotion the BIN
// phase performs. The engine binary is uploaded under a .tmp.<nonce> name so that replacing a
// running engine is an atomic rename; whatever is staged has to be moved into place by the same
// phase, because the install hook that used to do it is only called when a host is being
// initialized or upgraded, and a converge of a cluster already at the package's version is
// neither.
func TestStagedFilesSelectsOnlyWhatStillHasToMove(t *testing.T) {
	for _, tt := range []struct {
		name  string
		files []v1alpha1.File
		want  []string
	}{
		{
			name: "a staged executable is promoted to the target the package named",
			files: []v1alpha1.File{
				{
					Target:         "/usr/local/bin/k3s.tmp.1793045283940094960",
					OriginalTarget: "/usr/local/bin/k3s",
				},
			},
			want: []string{"/usr/local/bin/k3s"},
		},
		{
			name: "a file written straight to its target is already where it belongs",
			files: []v1alpha1.File{
				{
					Target:         "/etc/rancher/k3s/registries.yaml",
					OriginalTarget: "/etc/rancher/k3s/registries.yaml",
				},
			},
			want: []string{},
		},
		{
			name: "a file recorded with no original target is not moved onto an empty path",
			files: []v1alpha1.File{
				{
					Target:         "/usr/local/bin/k3s",
					OriginalTarget: "",
				},
			},
			want: []string{},
		},
		{
			name: "a mixed set promotes the staged half and leaves the rest alone",
			files: []v1alpha1.File{
				{
					Target:         "/usr/local/bin/k3s.tmp.1793045283940094960",
					OriginalTarget: "/usr/local/bin/k3s",
				},
				{
					Target:         "/etc/rancher/k3s/config.yaml",
					OriginalTarget: "/etc/rancher/k3s/config.yaml",
				},
				{
					Target:         "/usr/local/bin/k3s-killall.sh.tmp.1793045283940107494",
					OriginalTarget: "/usr/local/bin/k3s-killall.sh",
				},
			},
			want: []string{"/usr/local/bin/k3s", "/usr/local/bin/k3s-killall.sh"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := []string{}
			for _, f := range stagedFiles(tt.files) {
				got = append(got, f.OriginalTarget)
			}
			require.Equal(t, tt.want, got)
		})
	}
}
