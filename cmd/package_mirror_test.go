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

package cmd

import (
	"context"
	"testing"

	clusterapi "github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/stretchr/testify/require"
)

func registries(entries ...clusterapi.Registries) clusterapi.Cluster {
	c := clusterapi.Cluster{}
	c.Spec.Config.Registries = entries
	return c
}

func proxied(name, proxyURL string) clusterapi.Registries {
	return clusterapi.Registries{
		Name:  clusterapi.RegistryName(name),
		Proxy: &clusterapi.RegistryProxy{URL: proxyURL},
	}
}

// TestMirrorTargetFromCluster covers reading the destination out of the cluster configuration.
// The address has to come back in the form a reference takes -- host, plus a repository prefix
// when the proxy carries a path, and no scheme -- because it is concatenated with an image's own
// repository path to build what gets pushed.
func TestMirrorTargetFromCluster(t *testing.T) {
	tests := []struct {
		name    string
		cluster clusterapi.Cluster
		want    string
	}{
		{
			name:    "a bare host is completed to https and comes back without the scheme",
			cluster: registries(proxied("registry.k8s.io", "harbor.internal")),
			want:    "harbor.internal",
		},
		{
			name:    "an explicit scheme is stripped",
			cluster: registries(proxied("registry.k8s.io", "https://harbor.internal")),
			want:    "harbor.internal",
		},
		{
			name:    "a path on the proxy becomes the repository prefix",
			cluster: registries(proxied("registry.k8s.io", "https://harbor.internal/cargoship")),
			want:    "harbor.internal/cargoship",
		},
		{
			name:    "a port is part of the host",
			cluster: registries(proxied("registry.k8s.io", "http://registry.internal:5000")),
			want:    "registry.internal:5000",
		},
		{
			name:    "a trailing slash is not carried into the prefix",
			cluster: registries(proxied("registry.k8s.io", "https://harbor.internal/cargoship/")),
			want:    "harbor.internal/cargoship",
		},
		{
			name: "an entry with credentials but no proxy is not a destination",
			cluster: registries(
				clusterapi.Registries{
					Name:           "ghcr.io",
					Authentication: clusterapi.RegistryAuth{Username: "u", Password: "p"},
				},
				proxied("registry.k8s.io", "harbor.internal"),
			),
			want: "harbor.internal",
		},
		{
			name: "several registries proxied to one mirror resolve to it once",
			cluster: registries(
				proxied("registry.k8s.io", "harbor.internal/cargoship"),
			),
			want: "harbor.internal/cargoship",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mirrorTargetFromCluster(tt.cluster)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestMirrorTargetFromClusterErrors(t *testing.T) {
	t.Run("no proxy at all is an error rather than a guess", func(t *testing.T) {
		_, err := mirrorTargetFromCluster(registries(clusterapi.Registries{
			Name:           "ghcr.io",
			Authentication: clusterapi.RegistryAuth{Username: "u", Password: "p"},
		}))
		require.ErrorContains(t, err, "declares no registry proxy")
	})

	t.Run("an empty configuration is an error", func(t *testing.T) {
		_, err := mirrorTargetFromCluster(clusterapi.Cluster{})
		require.ErrorContains(t, err, "declares no registry proxy")
	})

	t.Run("two different mirrors must be disambiguated by the operator", func(t *testing.T) {
		// Guessing here would populate one mirror and leave the fleet pulling through the
		// other, which fails at pull time on a host rather than here.
		_, err := mirrorTargetFromCluster(registries(
			proxied("registry.k8s.io", "harbor.internal"),
			proxied("ghcr.io", "zot.internal"),
		))
		require.ErrorContains(t, err, "declares 2 registry proxies")
		require.ErrorContains(t, err, "harbor.internal")
		require.ErrorContains(t, err, "zot.internal")
	})
}

// TestResolveTarget covers how the argument and --config combine. They are alternatives, not
// layers: silently preferring one would mirror to an address the operator did not name.
func TestResolveTarget(t *testing.T) {
	ctx := context.Background()

	t.Run("an oci argument is used as given, with the prefix stripped", func(t *testing.T) {
		o := &packageMirrorOptions{}
		got, err := o.resolveTarget(ctx, []string{"pkg.tar.zst", "oci://harbor.internal/cargoship"})
		require.NoError(t, err)
		require.Equal(t, "harbor.internal/cargoship", got)
	})

	t.Run("an argument without the oci scheme is refused", func(t *testing.T) {
		o := &packageMirrorOptions{}
		_, err := o.resolveTarget(ctx, []string{"pkg.tar.zst", "harbor.internal"})
		require.ErrorContains(t, err, "oci://")
	})

	t.Run("naming both a registry and a cluster configuration is refused", func(t *testing.T) {
		o := &packageMirrorOptions{config: "cluster.yaml"}
		_, err := o.resolveTarget(ctx, []string{"pkg.tar.zst", "oci://harbor.internal"})
		require.ErrorContains(t, err, "not both")
	})

	t.Run("naming neither says how to name one", func(t *testing.T) {
		o := &packageMirrorOptions{}
		_, err := o.resolveTarget(ctx, []string{"pkg.tar.zst"})
		require.ErrorContains(t, err, "no destination registry")
	})
}
