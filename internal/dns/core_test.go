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

package dns

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseServiceURL(t *testing.T) {
	tests := []struct {
		name            string
		url             string
		wantNamespace   string
		wantServiceName string
		wantPort        int
		wantErr         string
	}{
		{
			name:            "valid service url",
			url:             "http://foo.bar.svc.cluster.local:8080",
			wantNamespace:   "bar",
			wantServiceName: "foo",
			wantPort:        8080,
		},
		{
			name:    "empty url",
			url:     "",
			wantErr: "service url cannot be empty",
		},
		{
			name:    "missing port",
			url:     "http://foo.bar.svc.cluster.local",
			wantErr: "does not have a port",
		},
		{
			name:    "port out of range",
			url:     "http://foo.bar.svc.cluster.local:70000",
			wantErr: "out of range",
		},
		{
			name:    "not a cluster-local hostname",
			url:     "http://example.com:8080",
			wantErr: "invalid service url",
		},
		{
			name:    "malformed url",
			url:     "http://[::1",
			wantErr: "missing ']'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			namespace, service, port, err := ParseServiceURL(tt.url)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantNamespace, namespace)
			require.Equal(t, tt.wantServiceName, service)
			require.Equal(t, tt.wantPort, port)
		})
	}
}

func TestIsServiceURL(t *testing.T) {
	require.True(t, IsServiceURL("http://foo.bar.svc.cluster.local:8080"))
	require.False(t, IsServiceURL("http://example.com:8080"))
	require.False(t, IsServiceURL(""))
}

func TestIsLocalhost(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{
			name: "localhost with port",
			url:  "localhost:8080",
			want: true,
		},
		{
			name: "loopback ipv4",
			url:  "127.0.0.1:8080",
			want: true,
		},
		{
			name: "loopback ipv6",
			url:  "[::1]:8080",
			want: true,
		},
		{
			name: "rfc1918 10.x",
			url:  "10.0.0.1:8080",
			want: true,
		},
		{
			name: "rfc1918 172.16.x",
			url:  "172.16.0.1:8080",
			want: true,
		},
		{
			name: "rfc1918 192.168.x",
			url:  "192.168.1.1:8080",
			want: true,
		},
		{
			name: ".local suffix",
			url:  "myhost.local",
			want: true,
		},
		{
			name: ".local with port",
			url:  "myhost.local:9000",
			want: true,
		},
		{
			name: "public ip",
			url:  "8.8.8.8:53",
			want: false,
		},
		{
			name: "public hostname",
			url:  "example.com:443",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, IsLocalhost(tt.url))
		})
	}
}
