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

package action

import (
	"testing"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/pkg/phase"
	"github.com/stretchr/testify/require"
)

// TestConstructorsRejectADistroTheyCannotBuild is the regression test for the panic in #825: every
// one of these constructors used to swallow the lookup error and return a bare nil, and every
// caller chained .Run(ctx) straight onto the result. The commands that take a distro package
// never reached it, because a package's spec.type is schema-constrained to a distro that exists;
// kube-config reads --distro instead, so an invocation with no flag and no config file panicked.
func TestConstructorsRejectADistroTheyCannotBuild(t *testing.T) {
	newManager := func(distroID string) *phase.Manager {
		return &phase.Manager{
			DistroID: distroID,
			Config:   &cluster.Cluster{},
		}
	}

	for _, tt := range []struct {
		name     string
		distroID string
		wantErr  error
	}{
		{
			name:     "no distro at all, which is what an unconfigured kube-config passes",
			distroID: "",
			wantErr:  ErrNoDistroSelected,
		},
		{
			name:     "a distro no module claims",
			distroID: "not-a-distro",
			wantErr:  nil,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			apply, err := NewApply(ApplyOptions{Manager: newManager(tt.distroID)})
			require.Error(t, err)
			require.Nil(t, apply)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			}

			kubeConfig, err := NewKubeConfig(KubeConfigOptions{Manager: newManager(tt.distroID)})
			require.Error(t, err)
			require.Nil(t, kubeConfig)

			sync, err := NewEngineConfigSync(EngineConfigSyncOptions{Manager: newManager(tt.distroID)})
			require.Error(t, err)
			require.Nil(t, sync)

			reset, err := NewReset(ResetOptions{Manager: newManager(tt.distroID)})
			require.Error(t, err)
			require.Nil(t, reset)
		})
	}
}

// TestDistroModuleBuildsARegisteredDistro is the other half: the error paths above are only
// useful if the success path still returns a module.
func TestDistroModuleBuildsARegisteredDistro(t *testing.T) {
	d, err := distroModule("k3s")
	require.NoError(t, err)
	require.NotNil(t, d)
}
