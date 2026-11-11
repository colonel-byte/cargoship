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

package zarfstate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDecode covers reading zarf's state JSON. The shape is zarf's, not ours, so these cases
// are the record of what the json tags on Registry are expected to match.
func TestDecode(t *testing.T) {
	tests := []struct {
		name      string
		stateJSON string
		want      Registry
		wantErr   string
	}{
		{
			name:      "a nodeport registry, as zarf init writes one",
			stateJSON: `{"distro":"k3s","registryInfo":{"pushUsername":"zarf-push","pushPassword":"secret","pullUsername":"zarf-pull","pullPassword":"other","address":"127.0.0.1:31999","port":31999,"nodePort":31999,"registryMode":"nodeport"}}`,
			want: Registry{
				Address:      "127.0.0.1:31999",
				Port:         31999,
				NodePort:     31999,
				PushUsername: "zarf-push",
				PushPassword: "secret",
				Mode:         ModeNodePort,
			},
		},
		{
			name:      "an external registry",
			stateJSON: `{"registryInfo":{"pushUsername":"robot","pushPassword":"p","address":"harbor.internal","registryMode":"external"}}`,
			want: Registry{
				Address:      "harbor.internal",
				PushUsername: "robot",
				PushPassword: "p",
				Mode:         ModeExternal,
			},
		},
		{
			name:      "state from an older zarf, which recorded the port as nodePort only",
			stateJSON: `{"registryInfo":{"pushUsername":"zarf-push","pushPassword":"secret","address":"127.0.0.1:31999","nodePort":31999}}`,
			want: Registry{
				Address:      "127.0.0.1:31999",
				Port:         31999,
				NodePort:     31999,
				PushUsername: "zarf-push",
				PushPassword: "secret",
			},
		},
		{
			name:      "no push credentials is an error, because that is the half worth reading",
			stateJSON: `{"registryInfo":{"address":"harbor.internal","registryMode":"external"}}`,
			wantErr:   "no registry push credentials",
		},
		{
			name:      "a push username with no password is incomplete, not partial",
			stateJSON: `{"registryInfo":{"pushUsername":"robot","address":"harbor.internal"}}`,
			wantErr:   "no registry push credentials",
		},
		{
			name:      "state that is not a zarf state at all",
			stateJSON: `not json`,
			wantErr:   "unable to read the zarf state",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decode([]byte(tt.stateJSON))
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestIsExternal(t *testing.T) {
	tests := []struct {
		name string
		reg  Registry
		want bool
	}{
		{
			name: "an external mode is external",
			reg: Registry{
				Mode:    ModeExternal,
				Address: "harbor.internal",
			},
			want: true,
		},
		{
			name: "a nodeport registry is internal",
			reg: Registry{
				Mode:    ModeNodePort,
				Address: "127.0.0.1:31999",
			},
			want: false,
		},
		{
			name: "a proxied registry is internal too, reached on a per-node host port",
			reg: Registry{
				Mode:    ModeProxy,
				Address: "127.0.0.1:31999",
			},
			want: false,
		},
		{
			name: "no mode and an address is how older zarf recorded an external registry",
			reg: Registry{
				Address: "harbor.internal",
			},
			want: true,
		},
		{
			name: "no mode and no address is not external",
			reg:  Registry{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.reg.IsExternal())
		})
	}
}

func TestUsesMTLS(t *testing.T) {
	require.False(t, Registry{}.UsesMTLS())
	require.False(t, Registry{MTLSStrategy: "none"}.UsesMTLS())
	require.True(t, Registry{MTLSStrategy: "zarf"}.UsesMTLS())
}
