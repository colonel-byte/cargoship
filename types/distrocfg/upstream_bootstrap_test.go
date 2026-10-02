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

package distrocfg

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
)

func TestParseJoinCommand(t *testing.T) {
	tests := []struct {
		name      string
		out       string
		wantToken string
		wantHash  string
		wantErr   bool
	}{
		{
			name: "real kubeadm init tail with line continuation",
			out: `Your Kubernetes control-plane has initialized successfully!

kubeadm join 10.0.0.1:6443 --token abcdef.0123456789abcdef \
	--discovery-token-ca-cert-hash sha256:deadbeef00112233445566778899aabbccddeeff00112233445566778899aa
`,
			wantToken: "abcdef.0123456789abcdef",
			wantHash:  "sha256:deadbeef00112233445566778899aabbccddeeff00112233445566778899aa",
		},
		{
			name:    "missing token",
			out:     "--discovery-token-ca-cert-hash sha256:abc",
			wantErr: true,
		},
		{
			name:    "missing hash",
			out:     "--token abcdef.0123456789abcdef",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, hash, err := parseJoinCommand(tt.out)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseJoinCommand() error = nil, want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseJoinCommand() error = %v, want nil", err)
			}
			if token != tt.wantToken || hash != tt.wantHash {
				t.Fatalf("parseJoinCommand() = (%q, %q), want (%q, %q)", token, hash, tt.wantToken, tt.wantHash)
			}
		})
	}
}

func TestUpstreamIsBootstrapped(t *testing.T) {
	d := newTestUpstream()

	notYet := (&fakeHost{fileExist: map[string]bool{}}).attach(&cluster.ZarfHost{})
	if d.IsBootstrapped(notYet) {
		t.Fatalf("IsBootstrapped() = true, want false when %s is absent", upstreamKubeletConfPath)
	}

	done := (&fakeHost{fileExist: map[string]bool{upstreamKubeletConfPath: true}}).attach(&cluster.ZarfHost{})
	if !d.IsBootstrapped(done) {
		t.Fatalf("IsBootstrapped() = false, want true when %s exists", upstreamKubeletConfPath)
	}
}

func TestUpstreamBootstrapLeaderNotConnected(t *testing.T) {
	d := newTestUpstream()
	host := &cluster.ZarfHost{Metadata: cluster.ZarfHostMetadata{IsLeader: true}}

	err := d.Bootstrap(context.Background(), host, cluster.ZarfRuntimeMeta{}, distro.ZarfDistro{})

	if !errors.Is(err, cluster.ErrNotConnected) {
		t.Fatalf("Bootstrap() error = %v, want %v", err, cluster.ErrNotConnected)
	}
}

func TestUpstreamBootstrapJoinNoLeader(t *testing.T) {
	d := newTestUpstream()
	host := &cluster.ZarfHost{Role: cluster.RoleWorker}

	err := d.Bootstrap(context.Background(), host, cluster.ZarfRuntimeMeta{}, distro.ZarfDistro{})

	if !errors.Is(err, ErrNoLeader) {
		t.Fatalf("Bootstrap() error = %v, want %v", err, ErrNoLeader)
	}
}

func TestUpstreamBootstrapJoinReadsTokenAndHashFromLeader(t *testing.T) {
	d := newTestUpstream()
	leaderCfg := &fakeHost{files: map[string]string{
		upstreamJoinTokenFile:  "worker-token.0123456789abcdef\n",
		upstreamCACertHashFile: "sha256:abc\n",
	}, fileExist: map[string]bool{}}
	leader := leaderCfg.attach(&cluster.ZarfHost{Hostname: "leader-1", Metadata: cluster.ZarfHostMetadata{IsLeader: true}})

	workerCfg := &fakeHost{fileExist: map[string]bool{}}
	worker := workerCfg.attach(&cluster.ZarfHost{Hostname: "worker-1", Role: cluster.RoleWorker})

	run := cluster.ZarfRuntimeMeta{Leader: leader, LoadBalancer: "lb.example.com"}

	err := d.Bootstrap(context.Background(), worker, run, distro.ZarfDistro{})

	if !errors.Is(err, cluster.ErrNotConnected) {
		t.Fatalf("Bootstrap() error = %v, want %v (kubeadm join itself is unmockable)", err, cluster.ErrNotConnected)
	}

	content, ok := workerCfg.files[kubeadmConfigPath]
	if !ok {
		t.Fatalf("Bootstrap() did not rewrite %s before joining, files = %+v", kubeadmConfigPath, workerCfg.files)
	}
	if !strings.Contains(content, "worker-token.0123456789abcdef") {
		t.Fatalf("kubeadm config = %q, want the real token from the leader", content)
	}
	if !strings.Contains(content, "caCertHashes") || !strings.Contains(content, "sha256:abc") {
		t.Fatalf("kubeadm config = %q, want the real CA cert hash from the leader", content)
	}
}
