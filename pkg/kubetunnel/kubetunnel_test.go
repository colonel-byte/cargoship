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

package kubetunnel

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
)

func TestPortForwardURL(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name      string
		host      string
		apiPath   string
		namespace string
		pod       string
		port      int
		want      string
		wantErr   string
	}{
		{
			name:      "default api path",
			host:      "https://127.0.0.1:6443",
			apiPath:   "",
			namespace: "zarf",
			pod:       "zarf-docker-registry-0",
			port:      5000,
			want:      "https://127.0.0.1:6443/api/v1/namespaces/zarf/pods/zarf-docker-registry-0/portforward?ports=5000",
		},
		{
			name:      "host carrying a path prefix",
			host:      "https://gateway.internal/k8s/clusters/c-m-abc",
			apiPath:   "/api",
			namespace: "zarf",
			pod:       "reg-0",
			port:      5000,
			want:      "https://gateway.internal/k8s/clusters/c-m-abc/api/v1/namespaces/zarf/pods/reg-0/portforward?ports=5000",
		},
		{
			name:      "no scheme",
			host:      "example.com/k8s",
			namespace: "zarf",
			pod:       "reg-0",
			port:      5000,
			wantErr:   "names no scheme",
		},
		{
			name:      "unparseable host",
			host:      "127.0.0.1:6443",
			namespace: "zarf",
			pod:       "reg-0",
			port:      5000,
			wantErr:   "unable to read the apiserver address",
		},
		{
			name:      "port out of range",
			host:      "https://127.0.0.1:6443",
			namespace: "zarf",
			pod:       "reg-0",
			port:      0,
			wantErr:   "out of range",
		},
		{
			name:    "no pod",
			host:    "https://127.0.0.1:6443",
			port:    5000,
			wantErr: "namespace and a pod name are required",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			u, err := portForwardURL(tt.host, tt.apiPath, tt.namespace, tt.pod, tt.port)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, u.String())
		})
	}
}

func TestPodBackingPort(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name     string
		raw      string
		wantPod  string
		wantPort int
		wantErr  string
	}{
		{
			name:     "one ready pod",
			raw:      `{"subsets":[{"addresses":[{"targetRef":{"kind":"Pod","name":"reg-0"}}],"ports":[{"port":5000}]}]}`,
			wantPod:  "reg-0",
			wantPort: 5000,
		},
		{
			name:     "target port differs from the service port",
			raw:      `{"subsets":[{"addresses":[{"targetRef":{"kind":"Pod","name":"reg-0"}}],"ports":[{"port":8080}]}]}`,
			wantPod:  "reg-0",
			wantPort: 8080,
		},
		{
			name:     "a subset with no ports is skipped",
			raw:      `{"subsets":[{"addresses":[{"targetRef":{"kind":"Pod","name":"skipped"}}]},{"addresses":[{"targetRef":{"kind":"Pod","name":"reg-1"}}],"ports":[{"port":5000}]}]}`,
			wantPod:  "reg-1",
			wantPort: 5000,
		},
		{
			name:    "an address backed by something other than a pod cannot be forwarded to",
			raw:     `{"subsets":[{"addresses":[{"targetRef":{"kind":"Node","name":"node-0"}}],"ports":[{"port":5000}]}]}`,
			wantErr: "no ready pod backing port 5000",
		},
		{
			name:    "no endpoints at all",
			raw:     `{"subsets":[]}`,
			wantErr: "no ready pod backing port 5000",
		},
		{
			name:    "a selectorless service has no subsets",
			raw:     `{"metadata":{"name":"zarf-connect-registry"}}`,
			wantErr: "no ready pod backing port 5000",
		},
		{
			name:    "unreadable body",
			raw:     `not json`,
			wantErr: "unable to read the endpoints",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pod, port, err := podBackingPort([]byte(tt.raw), "zarf", "zarf-docker-registry", 5000)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantPod, pod)
			require.Equal(t, tt.wantPort, port)
		})
	}
}

// TestDialRegistry drives a real tunnel against a real cluster, because the channel protocol is
// the part of this package a unit test cannot reach: a fake would assert that the framing matches
// itself rather than that it matches the API server.
//
// It needs a reachable cluster running an in-cluster registry, so it is opt-in.
func TestDialRegistry(t *testing.T) {
	ns, svc := os.Getenv("CARGOSHIP_KUBETUNNEL_NS"), os.Getenv("CARGOSHIP_KUBETUNNEL_SVC")
	if ns == "" || svc == "" {
		t.Skip("set CARGOSHIP_KUBETUNNEL_NS and CARGOSHIP_KUBETUNNEL_SVC to run this against a cluster")
	}

	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(), nil).ClientConfig()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dialer, err := To(ctx, cfg, ns, svc, 5000)
	require.NoError(t, err)
	t.Logf("dialing %s through the api server", dialer.Service())

	// The URL host is the one zarf state records and nothing dials it -- the dialer ignores
	// the address it is handed, which is the property this asserts. Verification is skipped
	// because the registry's certificate is for its in-cluster name; that is the operator's
	// call via --insecure-skip-tls-verify, and not what this test is about.
	client := &http.Client{Transport: &http.Transport{
		DialContext:     dialer.DialContext,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // see above
	}}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://127.0.0.1:5000/v2/", nil)
	require.NoError(t, err)
	if user, pass := os.Getenv("CARGOSHIP_KUBETUNNEL_USER"), os.Getenv("CARGOSHIP_KUBETUNNEL_PASS"); user != "" {
		req.SetBasicAuth(user, pass)
	}

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	t.Logf("GET /v2/ through the tunnel: %d %s", resp.StatusCode, body)
	require.Equal(t, http.StatusOK, resp.StatusCode, "the registry should answer /v2/ through the tunnel")
}
