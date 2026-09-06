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
	"context"
	"testing"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/stretchr/testify/require"
)

// An OS-specific upload phase claims a host by role, before it knows whether the package carries
// anything for that OS. Claiming a host it uploaded nothing to keeps the binary phase -- the
// catch-all for a package with no files for the host's OS -- from ever seeing it, and the host
// finishes the apply with no engine at all.
func TestUploadStepsClaimOnlyTheHostsItUploadsTo(t *testing.T) {
	uploaded := false
	upload := func(context.Context, *cluster.ZarfHost) error {
		uploaded = true
		return nil
	}

	for name, tt := range map[string]struct {
		files     []v1alpha1.ZarfFile
		wantClaim bool
	}{
		"a phase with files for the role claims the host": {
			files:     []v1alpha1.ZarfFile{{Name: "rke2-server.service"}},
			wantClaim: true,
		},
		"a phase with no files for the role leaves it for the binary phase": {
			files:     nil,
			wantClaim: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			uploaded = false
			p := &UploadFilesCommon{}
			h := &cluster.ZarfHost{Role: cluster.RoleController}

			for _, step := range p.uploadSteps(upload, tt.files) {
				require.NoError(t, step(context.Background(), h))
			}

			require.True(t, uploaded, "the upload step must run either way")
			require.Equal(t, tt.wantClaim, h.Metadata.EngineUploaded)
		})
	}
}
