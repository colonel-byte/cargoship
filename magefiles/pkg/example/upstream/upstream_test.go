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

package upstream

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testDebPackages = `Package: kubelet
Version: 1.35.2-1.1
Architecture: amd64
Filename: core/stable/v1.35/deb/kubelet_1.35.2-1.1_amd64.deb
SHA256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa

Package: kubelet
Version: 1.35.3-1.1
Architecture: amd64
Filename: core/stable/v1.35/deb/kubelet_1.35.3-1.1_amd64.deb
SHA256: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb

Package: kubelet
Version: 1.35.3-1.1
Architecture: arm64
Filename: core/stable/v1.35/deb/kubelet_1.35.3-1.1_arm64.deb
SHA256: cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
`

func TestParseDebPackages(t *testing.T) {
	stanzas := parseDebPackages([]byte(testDebPackages))
	if len(stanzas) != 3 {
		t.Fatalf("got %d stanzas, want 3", len(stanzas))
	}
	if stanzas[1].Version != "1.35.3-1.1" || stanzas[1].Architecture != "amd64" {
		t.Errorf("stanza[1] = %+v, want version 1.35.3-1.1 amd64", stanzas[1])
	}
}

func TestNewestDebStanza(t *testing.T) {
	stanzas := parseDebPackages([]byte(testDebPackages))

	best, err := newestDebStanza(stanzas, "kubelet", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if best.Version != "1.35.3-1.1" {
		t.Errorf("got version %q, want the newer 1.35.3-1.1 over 1.35.2-1.1", best.Version)
	}
	if best.SHA256 != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Errorf("got sha256 %q for the wrong stanza", best.SHA256)
	}

	if _, err := newestDebStanza(stanzas, "kubelet", "riscv64"); err == nil {
		t.Error("expected an error for an architecture with no stanza")
	}
}

const testKubeadmConstants = `package constants

const (
	// DefaultEtcdVersion is etcd's pinned version.
	DefaultEtcdVersion = "3.6.6-0"
	// CoreDNSVersion is CoreDNS's pinned version.
	CoreDNSVersion = "v1.13.1"
	// PauseVersion is the pause image's pinned version.
	PauseVersion = "3.10.1"
)
`

func TestKubeadmConstants(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/kubernetes/kubernetes/v1.35.8/cmd/kubeadm/app/constants/constants.go", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, testKubeadmConstants)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	etcd, coredns, pause, err := fetchKubeadmConstants(srv.URL, "v1.35.8")
	if err != nil {
		t.Fatal(err)
	}
	if etcd != "3.6.6-0" {
		t.Errorf("got etcd version %q, want 3.6.6-0", etcd)
	}
	if coredns != "v1.13.1" {
		t.Errorf("got coredns version %q, want v1.13.1", coredns)
	}
	if pause != "3.10.1" {
		t.Errorf("got pause version %q, want 3.10.1", pause)
	}
}

func TestPickDebStanza(t *testing.T) {
	stanzas := parseDebPackages([]byte(testDebPackages))

	exact, err := pickDebStanza(stanzas, "kubelet", "amd64", "1.35.2")
	if err != nil {
		t.Fatal(err)
	}
	if exact.Version != "1.35.2-1.1" {
		t.Errorf("got version %q, want the exact match 1.35.2-1.1", exact.Version)
	}

	closest, err := pickDebStanza(stanzas, "kubelet", "amd64", "1.35.9")
	if err != nil {
		t.Fatal(err)
	}
	if closest.Version != "1.35.3-1.1" {
		t.Errorf("got version %q, want the closest-below 1.35.3-1.1", closest.Version)
	}

	if _, err := pickDebStanza(stanzas, "kubelet", "amd64", "1.35.1"); err == nil {
		t.Error("expected an error when every candidate is newer than target")
	}
}
