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
	"github.com/colonel-byte/cargoship/internal/zarfstate"
	"github.com/k0sproject/rig/v2/protocol/openssh"
	rigssh "github.com/k0sproject/rig/v2/protocol/ssh"
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
		require.Equal(t, mirrorTarget{registry: "harbor.internal/cargoship"}, got)
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

	t.Run("a context with nothing to read a cluster for is refused, not ignored", func(t *testing.T) {
		o := &packageMirrorOptions{kubeContext: "prod"}
		_, err := o.resolveTarget(ctx, []string{"pkg.tar.zst", "oci://harbor.internal"})
		require.ErrorContains(t, err, "--from-zarf-state")
	})
}

// TestRegistryAddress covers the address decision on its own, including the node-local case the
// non-tunnelled path can only report as an error.
func TestRegistryAddress(t *testing.T) {
	tests := []struct {
		name          string
		reg           zarfstate.Registry
		explicit      string
		wantAddr      string
		wantNodeLocal bool
		wantErr       string
	}{
		{
			name: "a nodeport address is used as recorded when it already carries a port",
			reg: zarfstate.Registry{
				Address: "127.0.0.1:31999",
				Port:    31999,
				Mode:    zarfstate.ModeNodePort,
			},
			wantAddr:      "127.0.0.1:31999",
			wantNodeLocal: true,
		},
		{
			name: "a recorded address with no port takes the one from state",
			reg: zarfstate.Registry{
				Address: "127.0.0.1",
				Port:    31999,
				Mode:    zarfstate.ModeNodePort,
			},
			wantAddr:      "127.0.0.1:31999",
			wantNodeLocal: true,
		},
		{
			name: "a proxy-mode host port is node-local too",
			reg: zarfstate.Registry{
				Address: "127.0.0.1:5000",
				Port:    5000,
				Mode:    zarfstate.ModeProxy,
			},
			wantAddr:      "127.0.0.1:5000",
			wantNodeLocal: true,
		},
		{
			name: "no address and no port leaves nothing to dial",
			reg: zarfstate.Registry{
				Mode: zarfstate.ModeNodePort,
			},
			wantErr: "nothing to reach it on",
		},
		{
			name: "an explicit address is this machine's business, never the tunnel's",
			reg: zarfstate.Registry{
				Address: "127.0.0.1:31999",
				Port:    31999,
				Mode:    zarfstate.ModeNodePort,
			},
			explicit:      "node1.internal:31999",
			wantAddr:      "node1.internal:31999",
			wantNodeLocal: false,
		},
		{
			name: "an external registry needs no tunnel",
			reg: zarfstate.Registry{
				Address: "harbor.internal",
				Mode:    zarfstate.ModeExternal,
			},
			wantAddr:      "harbor.internal",
			wantNodeLocal: false,
		},
		{
			// mTLS changes the scheme and the trust, not the address. This used to be
			// refused outright on the premise that a push would need a client certificate:
			// zarf issues one for the per-node registry proxy, not for a direct client, and
			// the registry accepts a push authenticated with the push credentials alone.
			name: "zarf-managed mtls leaves the address alone",
			reg: zarfstate.Registry{
				Address:      "harbor.internal",
				PushUsername: "zarf-push",
				PushPassword: "secret",
				Mode:         zarfstate.ModeExternal,
				MTLSStrategy: "zarf-managed",
			},
			wantAddr: "harbor.internal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, nodeLocal, err := registryAddress(tt.reg, tt.explicit)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantAddr, addr)
			require.Equal(t, tt.wantNodeLocal, nodeLocal)
		})
	}
}

// TestFirstControllerWithSSH covers which node a tunnel is opened through. Any controller will
// do -- a NodePort answers on every node -- so the only real decisions are skipping workers and
// refusing a fleet whose hosts cargoship cannot open a channel to.
func TestFirstControllerWithSSH(t *testing.T) {
	sshHost := func(role, addr string) *clusterapi.Host {
		h := &clusterapi.Host{Role: role}
		h.ConnectionConfig.SSH = &rigssh.Config{Address: addr}
		return h
	}
	openSSHHost := func(role, addr string) *clusterapi.Host {
		h := &clusterapi.Host{Role: role}
		h.ConnectionConfig.OpenSSH = &openssh.Config{Address: addr}
		return h
	}

	tests := []struct {
		name    string
		hosts   []*clusterapi.Host
		want    string
		wantErr string
	}{
		{
			name: "the first controller is taken",
			hosts: []*clusterapi.Host{
				sshHost(clusterapi.RoleWorker, "worker1"),
				sshHost(clusterapi.RoleController, "control1"),
				sshHost(clusterapi.RoleController, "control2"),
			},
			want: "control1",
		},
		{
			name: "a single-role host counts as a controller",
			hosts: []*clusterapi.Host{
				sshHost(clusterapi.RoleSingle, "solo"),
			},
			want: "solo",
		},
		{
			name: "a controller with no ssh block is skipped for one that has it",
			hosts: []*clusterapi.Host{
				openSSHHost(clusterapi.RoleController, "control1"),
				sshHost(clusterapi.RoleController, "control2"),
			},
			want: "control2",
		},
		{
			name: "a fleet cargoship cannot open a channel to is refused by name",
			hosts: []*clusterapi.Host{
				openSSHHost(clusterapi.RoleController, "control1"),
			},
			wantErr: "ssh",
		},
		{
			name: "workers alone leave nothing to tunnel through",
			hosts: []*clusterapi.Host{
				sshHost(clusterapi.RoleWorker, "worker1"),
			},
			wantErr: "names no controller",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := clusterapi.Cluster{}
			c.Spec.Hosts = tt.hosts

			got, err := firstControllerWithSSH(c)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got.ConnectionConfig.SSH.Address)
		})
	}
}

// TestTargetFromZarfRegistry covers how a zarf deployment's state becomes a destination. The
// credentials always come from state; the address only does when a machine other than a node
// can reach it.
func TestTargetFromZarfRegistry(t *testing.T) {
	internal := zarfstate.Registry{
		Address:      "127.0.0.1:31999",
		Port:         31999,
		PushUsername: "zarf-push",
		PushPassword: "secret",
		Mode:         zarfstate.ModeNodePort,
	}

	tests := []struct {
		name     string
		reg      zarfstate.Registry
		explicit string
		want     mirrorTarget
		wantErr  string
	}{
		{
			name: "an external registry is taken whole",
			reg: zarfstate.Registry{
				Address:      "harbor.internal",
				PushUsername: "zarf-push",
				PushPassword: "secret",
				Mode:         zarfstate.ModeExternal,
			},
			want: mirrorTarget{
				registry: "harbor.internal",
				username: "zarf-push",
				password: "secret",
			},
		},
		{
			name: "state written before zarf recorded a mode is external when it names an address",
			reg: zarfstate.Registry{
				Address:      "harbor.internal",
				PushUsername: "zarf-push",
				PushPassword: "secret",
			},
			want: mirrorTarget{
				registry: "harbor.internal",
				username: "zarf-push",
				password: "secret",
			},
		},
		{
			name:     "an explicit address wins, and the credentials still come from state",
			reg:      internal,
			explicit: "node1.internal:31999",
			want: mirrorTarget{
				registry: "node1.internal:31999",
				username: "zarf-push",
				password: "secret",
			},
		},
		{
			name:    "a nodeport registry with no address given names the fix",
			reg:     internal,
			wantErr: "only a node can reach",
		},
		{
			name: "a proxied registry is no more reachable than a nodeport one",
			reg: zarfstate.Registry{
				Address:      "127.0.0.1:31999",
				Port:         31999,
				PushUsername: "zarf-push",
				PushPassword: "secret",
				Mode:         zarfstate.ModeProxy,
			},
			wantErr: "only a node can reach",
		},
		{
			name: "an external registry with no address at all is an error",
			reg: zarfstate.Registry{
				PushUsername: "zarf-push",
				PushPassword: "secret",
				Mode:         zarfstate.ModeExternal,
			},
			wantErr: "records no registry address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &packageMirrorOptions{}
			got, err := o.targetFromZarfRegistry(context.Background(), tt.reg, tt.explicit)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
