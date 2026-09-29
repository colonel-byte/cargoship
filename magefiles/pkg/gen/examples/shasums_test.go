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
