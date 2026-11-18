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
	"encoding/json"
	"sort"
	"testing"
)

func TestExampleShasumsRecord(t *testing.T) {
	s := &exampleShasums{sums: map[string]exampleShasum{}}

	s.record("https://example.com/pkg-1.0.deb", "aaaa")
	if !s.dirty {
		t.Fatal("expected record of a new url to mark the cache dirty")
	}
	sum, ok := s.lookup("https://example.com/pkg-1.0.deb")
	if !ok || sum.SHA256 != "aaaa" {
		t.Fatalf("got %+v, %v, want sha256 aaaa", sum, ok)
	}

	s.dirty = false
	s.record("https://example.com/pkg-1.0.deb", "aaaa")
	if s.dirty {
		t.Error("expected recording an unchanged digest to be a no-op")
	}

	s.record("https://example.com/pkg-1.0.deb", "bbbb")
	if !s.dirty {
		t.Error("expected recording a changed digest to mark the cache dirty")
	}
	sum, ok = s.lookup("https://example.com/pkg-1.0.deb")
	if !ok || sum.SHA256 != "bbbb" {
		t.Fatalf("got %+v, %v, want sha256 bbbb after overwrite", sum, ok)
	}
}

// TestExampleShasumsRecordMultipleURLs covers pkgs.k8s.io's quirk: a package that hasn't
// changed between k8s minors is mirrored, byte for byte, into every later minor's own repo
// path -- so the same file name is genuinely reachable at more than one URL.
func TestExampleShasumsRecordMultipleURLs(t *testing.T) {
	s := &exampleShasums{sums: map[string]exampleShasum{}}
	urlV136 := "https://pkgs.k8s.io/core:/stable:/v1.36/rpm/aarch64/kubernetes-cni-1.9.1-150500.1.1.aarch64.rpm"
	urlV137 := "https://pkgs.k8s.io/core:/stable:/v1.37/rpm/aarch64/kubernetes-cni-1.9.1-150500.1.1.aarch64.rpm"

	s.record(urlV136, "cccc")
	s.record(urlV137, "cccc")

	if e, ok := s.lookup(urlV136); !ok || e.SHA256 != "cccc" {
		t.Fatalf("lost the v1.36 URL after recording v1.37's copy: %+v, %v", e, ok)
	}
	if e, ok := s.lookup(urlV137); !ok || e.SHA256 != "cccc" {
		t.Fatalf("v1.37 URL not recorded: %+v, %v", e, ok)
	}

	e := s.sums["kubernetes-cni-1.9.1-150500.1.1.aarch64.rpm"]
	if len(e.URLs) != 2 {
		t.Fatalf("got %d URLs, want both v1.36 and v1.37 kept: %+v", len(e.URLs), e.URLs)
	}
}

// TestExampleShasumUnmarshalJSONMigratesSingleURL covers loading a shasums.json written before
// entries could hold more than one URL.
func TestExampleShasumUnmarshalJSONMigratesSingleURL(t *testing.T) {
	var e exampleShasum
	if err := json.Unmarshal([]byte(`{"url":"https://example.com/pkg-1.0.deb","sha256":"aaaa"}`), &e); err != nil {
		t.Fatal(err)
	}
	if e.SHA256 != "aaaa" || len(e.URLs) != 1 || e.URLs[0] != "https://example.com/pkg-1.0.deb" {
		t.Fatalf("got %+v, want it migrated into URLs", e)
	}
}

// TestFetchK3sBinarySHAReadsTheCache covers the digests that used to be cached as whole
// sha256sum-<arch>.txt assets and now live here, keyed by release tag so that every version's
// plainly-named binary gets its own entry. A cache hit must answer without a request, which is
// what makes a fresh clone able to re-render the k3s examples offline -- so the repo URL here
// is one that would fail if it were ever reached.
func TestFetchK3sBinarySHAReadsTheCache(t *testing.T) {
	const (
		tag      = "v1.35.0%2Bk3s1"
		amdURL   = "https://github.com/k3s-io/k3s/releases/download/" + tag + "/k3s"
		armURL   = "https://github.com/k3s-io/k3s/releases/download/" + tag + "/k3s-arm64"
		amdSum   = "aaaa"
		armSum   = "bbbb"
		noRemote = "https://127.0.0.1:1/k3s-io/k3s"
	)

	s := &exampleShasums{sums: map[string]exampleShasum{}}
	s.storeKey(archiveKey(amdURL), amdURL, amdSum)
	s.storeKey(archiveKey(armURL), armURL, armSum)

	if _, ok := s.sums[tag+"/k3s"]; !ok {
		t.Fatalf("want an entry keyed by tag and binary name, got keys %v", s.sums)
	}

	v := exampleVersion{
		TagURL: tag,
		Arches: []exampleArch{
			{
				Arch:      "amd64",
				BinaryURL: amdURL,
			},
			{
				Arch:      "arm64",
				BinaryURL: armURL,
			},
		},
	}
	if err := fetchK3sBinarySHA(&v, noRemote, s); err != nil {
		t.Fatalf("expected the cache to answer without a fetch: %v", err)
	}
	if v.Arches[0].BinarySHA != amdSum || v.Arches[1].BinarySHA != armSum {
		t.Fatalf("got %q and %q, want %q and %q",
			v.Arches[0].BinarySHA, v.Arches[1].BinarySHA, amdSum, armSum)
	}
}

// Every rke2 release publishes its tarball under the one name rke2.linux-amd64.tar.gz, and the
// zarf definitions hash the whole archive -- so with one entry per name each rendered version
// evicted the last, the committed cache held whichever was rendered last, and every run
// re-downloaded a 230MiB tarball once per version. A name holding different content falls back
// to a version-qualified key so the versions coexist.
func TestExampleShasumsVersionedFallback(t *testing.T) {
	s := &exampleShasums{sums: map[string]exampleShasum{}}
	r1 := "https://github.com/rancher/rke2/releases/download/v1.37.1%2Brke2r1/rke2.linux-amd64.tar.gz"
	r2 := "https://github.com/rancher/rke2/releases/download/v1.37.2%2Brke2r1/rke2.linux-amd64.tar.gz"

	s.record(r1, "aaaa")
	s.record(r2, "bbbb")

	if e, ok := s.lookup(r1); !ok || e.SHA256 != "aaaa" {
		t.Fatalf("the first release's digest was evicted by the second: %+v, %v", e, ok)
	}
	if e, ok := s.lookup(r2); !ok || e.SHA256 != "bbbb" {
		t.Fatalf("the second release's digest was not recorded: %+v, %v", e, ok)
	}

	// The first keeps the plain name; the second takes the version-qualified key, which is the
	// one archiveKey builds and the one memberKey has always used for this archive's members.
	if _, ok := s.sums["rke2.linux-amd64.tar.gz"]; !ok {
		t.Error("the first release should keep the plain file-name key")
	}
	if _, ok := s.sums["v1.37.2%2Brke2r1/rke2.linux-amd64.tar.gz"]; !ok {
		t.Errorf("the second release has no version-qualified entry: %v", cacheKeys(s))
	}
}

// A URL already recorded that now serves different bytes is upstream re-cutting an asset in
// place, which replaces the entry rather than growing a second one: there is only one file at
// that URL, and the old digest is simply wrong now.
func TestExampleShasumsReplacesARecutAsset(t *testing.T) {
	s := &exampleShasums{sums: map[string]exampleShasum{}}
	url := "https://example.com/releases/v1/artifact.tar.gz"

	s.record(url, "aaaa")
	s.record(url, "bbbb")

	if len(s.sums) != 1 {
		t.Fatalf("got %d entries, want the re-cut asset to replace the first: %v", len(s.sums), cacheKeys(s))
	}
	if e, ok := s.lookup(url); !ok || e.SHA256 != "bbbb" {
		t.Fatalf("got %+v, %v, want the new digest", e, ok)
	}
}

// cacheKeys names what the cache holds, for a failure message.
func cacheKeys(s *exampleShasums) []string {
	keys := make([]string, 0, len(s.sums))
	for k := range s.sums {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestFetchRKE2TarballSHAReadsTheCache is TestFetchK3sBinarySHAReadsTheCache for the rke2
// tarball: a cached digest must answer without a request, which is what lets a fresh clone
// re-render the rke2 examples offline. The repo URL is one that fails if it is ever reached.
func TestFetchRKE2TarballSHAReadsTheCache(t *testing.T) {
	const (
		tag      = "v1.35.0%2Brke2r1"
		amdURL   = "https://github.com/rancher/rke2/releases/download/" + tag + "/rke2.linux-amd64.tar.gz"
		amdSum   = "aaaa"
		noRemote = "https://127.0.0.1:1/rancher/rke2"
	)

	s := &exampleShasums{sums: map[string]exampleShasum{}}
	s.storeKey(archiveKey(amdURL), amdURL, amdSum)
	if _, ok := s.sums[tag+"/rke2.linux-amd64.tar.gz"]; !ok {
		t.Fatalf("want an entry keyed by tag and tarball name, got keys %v", s.sums)
	}

	v := exampleVersion{
		TagURL: tag,
		Arches: []exampleArch{
			{
				Arch:       "amd64",
				Tarball:    "rke2.linux-amd64.tar.gz",
				TarballURL: amdURL,
			},
		},
	}
	if err := fetchRKE2TarballSHA(&v, noRemote, s); err != nil {
		t.Fatalf("expected the cache to answer without a fetch: %v", err)
	}
}

// A digest cached under the bare tarball name -- the shape the cache used before entries were
// version-qualified -- must not satisfy the lookup, or the one release that happens to hold
// that key keeps it forever and the committed cache stops being reproducible from an empty
// one. Reaching for the network here is the correct behaviour, and the unreachable host is
// what proves the lookup missed.
func TestFetchRKE2TarballSHAIgnoresALegacyBareKey(t *testing.T) {
	const (
		tag      = "v1.35.0%2Brke2r1"
		amdURL   = "https://github.com/rancher/rke2/releases/download/" + tag + "/rke2.linux-amd64.tar.gz"
		noRemote = "https://127.0.0.1:1/rancher/rke2"
	)

	s := &exampleShasums{sums: map[string]exampleShasum{}}
	s.storeKey("rke2.linux-amd64.tar.gz", amdURL, "aaaa")

	v := exampleVersion{
		TagURL: tag,
		Arches: []exampleArch{
			{
				Arch:       "amd64",
				Tarball:    "rke2.linux-amd64.tar.gz",
				TarballURL: amdURL,
			},
		},
	}
	// A transport failure is returned rather than warned past, so a 502 from the release host
	// cannot silently restore the 230MiB download this function exists to avoid.
	if err := fetchRKE2TarballSHA(&v, noRemote, s); err == nil {
		t.Fatal("want the bare-name entry ignored and the fetch attempted, got a cache hit")
	}
}
