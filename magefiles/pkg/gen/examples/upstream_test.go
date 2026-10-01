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

package examples

import (
	"compress/gzip"
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
		if _, err := w.Write([]byte(testKubeadmConstants)); err != nil {
			t.Errorf("writing test response: %v", err)
		}
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

const testPrimaryXML = `<?xml version="1.0" encoding="UTF-8"?>
<metadata xmlns="http://linux.duke.edu/metadata/common" packages="2">
  <package type="rpm">
    <name>kubelet</name>
    <arch>x86_64</arch>
    <version epoch="0" ver="1.35.2" rel="0"/>
    <checksum type="sha256" pkgid="YES">dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd</checksum>
    <location href="Packages/k/kubelet-1.35.2-0.x86_64.rpm"/>
  </package>
  <package type="rpm">
    <name>kubelet</name>
    <arch>x86_64</arch>
    <version epoch="0" ver="1.35.3" rel="0"/>
    <checksum type="sha256" pkgid="YES">eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee</checksum>
    <location href="Packages/k/kubelet-1.35.3-0.x86_64.rpm"/>
  </package>
</metadata>
`

// newTestRPMRepo serves repomd.xml + a gzip primary.xml off a local httptest server, in the same
// directory shape fetchRPMPrimary expects (baseURL/repodata/*).
func newTestRPMRepo(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repodata/repomd.xml", func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<repomd xmlns="http://linux.duke.edu/metadata/repo">
  <data type="primary">
    <location href="repodata/primary.xml.gz"/>
  </data>
</repomd>
`)); err != nil {
			t.Errorf("writing test response: %v", err)
		}
	})
	mux.HandleFunc("/repodata/primary.xml.gz", func(w http.ResponseWriter, _ *http.Request) {
		gz := gzip.NewWriter(w)
		if _, err := gz.Write([]byte(testPrimaryXML)); err != nil {
			t.Errorf("writing test response: %v", err)
		}
		if err := gz.Close(); err != nil {
			t.Errorf("closing test response: %v", err)
		}
	})
	return httptest.NewServer(mux)
}

func TestFetchRPMPrimaryAndFindLatestRPMArch(t *testing.T) {
	srv := newTestRPMRepo(t)
	defer srv.Close()

	primary, err := fetchRPMPrimary(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	pkg, err := findLatestRPMArch(primary, "kubelet", "x86_64")
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Version.Ver != "1.35.3" {
		t.Errorf("got version %q, want the newer 1.35.3 over 1.35.2", pkg.Version.Ver)
	}
	if pkg.Location.Href != "Packages/k/kubelet-1.35.3-0.x86_64.rpm" {
		t.Errorf("got href %q for the wrong package", pkg.Location.Href)
	}

	if _, err := findLatestRPMArch(primary, "kubelet", "aarch64"); err == nil {
		t.Error("expected an error for an architecture with no package")
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

func TestPickRPMArch(t *testing.T) {
	srv := newTestRPMRepo(t)
	defer srv.Close()

	primary, err := fetchRPMPrimary(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	exact, err := pickRPMArch(primary, "kubelet", "x86_64", "1.35.2")
	if err != nil {
		t.Fatal(err)
	}
	if exact.Version.Ver != "1.35.2" {
		t.Errorf("got version %q, want the exact match 1.35.2", exact.Version.Ver)
	}

	closest, err := pickRPMArch(primary, "kubelet", "x86_64", "1.35.9")
	if err != nil {
		t.Fatal(err)
	}
	if closest.Version.Ver != "1.35.3" {
		t.Errorf("got version %q, want the closest-below 1.35.3", closest.Version.Ver)
	}

	if _, err := pickRPMArch(primary, "kubelet", "x86_64", "1.35.1"); err == nil {
		t.Error("expected an error when every candidate is newer than target")
	}
}
