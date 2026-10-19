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

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	hostos "github.com/colonel-byte/cargoship/types/os"
	"github.com/stretchr/testify/require"
)

// fqdnConfigurer reports the name a machine with a search domain gives for itself, which is not
// the name its node carries in the cluster. That gap is the whole defect: see resolveNodeNames.
type fqdnConfigurer struct {
	hostos.Configurer
}

func (c *fqdnConfigurer) Hostname(_ hostos.Host) string { return "kc0.cargoship.test" }

func TestNodeNameForPrefersWhatTheClusterCallsTheNode(t *testing.T) {
	t.Parallel()

	h := &cluster.Host{
		Hostname:   "kc0",
		Configurer: &fqdnConfigurer{},
	}

	tests := []struct {
		name  string
		names map[string]string
		want  string
	}{
		{
			name:  "the resolved name wins over the one the machine reports",
			names: map[string]string{h.String(): "kc0"},
			want:  "kc0",
		},
		{
			name:  "an unresolved host falls back to the reported name",
			names: nil,
			want:  "kc0.cargoship.test",
		},
		{
			name:  "a host missing from a resolved map falls back too",
			names: map[string]string{"other": "kw0"},
			want:  "kc0.cargoship.test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, nodeNameFor(tt.names, h))
		})
	}
}

// TestNodeNameResolvesWithoutALeader covers the lazy resolution's one hostile input: a phase
// whose leader was never set still has to name a node rather than panic reaching for a
// connection that is not there.
func TestNodeNameResolvesWithoutALeader(t *testing.T) {
	t.Parallel()

	h := &cluster.Host{
		Hostname:   "kc0",
		Configurer: &fqdnConfigurer{},
	}

	upgrade := &UpgradeHosts{}
	require.Equal(t, "kc0.cargoship.test", upgrade.nodeName(context.Background(), h))
	// The second call must not resolve again, which is what the sync.Once buys the parallel
	// worker batches.
	require.Equal(t, "kc0.cargoship.test", upgrade.nodeName(context.Background(), h))

	sync := &EngineConfigSyncHosts{}
	require.Equal(t, "kc0.cargoship.test", sync.nodeName(context.Background(), h))
}
