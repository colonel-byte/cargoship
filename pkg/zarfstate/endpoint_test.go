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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAPIEndpoint covers deriving the tunnel's target from a kubeconfig. The port is the whole
// point: it is read rather than assumed, because tunnelling to a port the API server does not
// listen on fails in a way that looks like a broken tunnel rather than a wrong guess.
func TestAPIEndpoint(t *testing.T) {
	tests := []struct {
		name   string
		server string
		want   string
	}{
		{
			name:   "the port a kubeconfig names is the port tunnelled to",
			server: "https://control.internal:6443",
			want:   "127.0.0.1:6443",
		},
		{
			name:   "a cluster behind a load balancer on another port keeps that port",
			server: "https://api.example.com:8443",
			want:   "127.0.0.1:8443",
		},
		{
			name:   "no port means the https default",
			server: "https://api.example.com",
			want:   "127.0.0.1:443",
		},
		{
			name:   "an address that is already loopback is not special",
			server: "https://127.0.0.1:6443",
			want:   "127.0.0.1:6443",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := APIEndpoint(writeKubeconfig(t, tt.server), "")
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestAPIEndpointHonoursTheContext checks that the server is read from the named context rather
// than the current one, since an operator with several clusters is exactly who passes --context.
func TestAPIEndpointHonoursTheContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(path, []byte(`apiVersion: v1
kind: Config
current-context: dev
clusters:
  - name: dev
    cluster:
      server: https://dev.internal:6443
  - name: prod
    cluster:
      server: https://prod.internal:7443
contexts:
  - name: dev
    context:
      cluster: dev
      user: dev
  - name: prod
    context:
      cluster: prod
      user: prod
users:
  - name: dev
    user:
      token: dev-token
  - name: prod
    user:
      token: prod-token
`), 0o600))

	current, err := APIEndpoint(path, "")
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:6443", current)

	named, err := APIEndpoint(path, "prod")
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:7443", named)

	_, err = APIEndpoint(path, "nonexistent")
	require.ErrorContains(t, err, "nonexistent")
}

func writeKubeconfig(t *testing.T, server string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(path, []byte(`apiVersion: v1
kind: Config
current-context: test
clusters:
  - name: test
    cluster:
      server: `+server+`
contexts:
  - name: test
    context:
      cluster: test
      user: test
users:
  - name: test
    user:
      token: test-token
`), 0o600))

	return path
}
