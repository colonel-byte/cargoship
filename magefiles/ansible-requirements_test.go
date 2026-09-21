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

package main

import (
	"strings"
	"testing"
)

// mustRange parses a specifier the test asserts is valid.
func mustRange(t *testing.T, spec string) versionRange {
	t.Helper()

	rng, err := parseSpecifier(spec)
	if err != nil {
		t.Fatalf("parseSpecifier(%q): %v", spec, err)
	}
	return rng
}

// TestParseSpecifierRejectsABareVersion pins the defect this tooling was built around: meta/
// runtime.yml held requires_ansible: "2.16.16", which is not a PEP 440 specifier. ansible-core
// downgrades the parse failure to a warning, so the declared floor enforced nothing.
func TestParseSpecifierRejectsABareVersion(t *testing.T) {
	_, err := parseSpecifier("2.16.16")
	if err == nil {
		t.Fatal("parseSpecifier(\"2.16.16\"): got nil error, want a rejection")
	}
	// The message has to name the fix: this value is what an ansible-core release number
	// looks like when copied straight in, and the reader needs to know to add an operator.
	if !strings.Contains(err.Error(), ">=2.16.16") {
		t.Errorf("parseSpecifier(\"2.16.16\"): error %q does not suggest an operator", err)
	}
}

func TestParseSpecifier(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		want    string
		wantErr bool
	}{
		{name: "empty is unbounded", spec: "", want: "*"},
		{name: "floor only", spec: ">=2.16.0", want: ">=2.16.0"},
		{name: "partial version fills out", spec: ">=2.9", want: ">=2.9.0"},
		{name: "floor and ceiling", spec: ">=2.9,<2.11", want: ">=2.9.0,<2.11.0"},
		{name: "whitespace around clauses", spec: ">=2.16.0 , <2.17.0", want: ">=2.16.0,<2.17.0"},
		{name: "exclusive floor", spec: ">2.16.0", want: ">2.16.0"},
		{name: "equality is a point", spec: "==2.16.16", want: ">=2.16.16,<=2.16.16"},
		{name: "tightest floor wins", spec: ">=2.9,>=2.16.0", want: ">=2.16.0"},
		{name: "tightest ceiling wins", spec: "<2.19,<2.17.0", want: "<2.17.0"},
		{name: "bare version", spec: "2.16.16", wantErr: true},
		{name: "operator with no version", spec: ">=", wantErr: true},
		{name: "unreducible operator", spec: "~=2.16.0", wantErr: true},
		{name: "exclusion is not an interval", spec: "!=2.16.0", wantErr: true},
		{name: "unparseable version", spec: ">=two", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSpecifier(tt.spec)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseSpecifier(%q): got %s, want an error", tt.spec, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSpecifier(%q): %v", tt.spec, err)
			}
			if got.String() != tt.want {
				t.Errorf("parseSpecifier(%q) = %s, want %s", tt.spec, got, tt.want)
			}
		})
	}
}

// TestVersionRangeAdmits covers containment rather than overlap. A release the controller range
// only partly fits is rejected: an operator anywhere in the declared range has to be able to run
// the pin.
func TestVersionRangeAdmits(t *testing.T) {
	const controllerSpec = ">=2.16.0,<2.17.0"

	tests := []struct {
		name    string
		release string
		want    bool
	}{
		{name: "unconstrained release", release: "", want: true},
		{name: "floor below the controller", release: ">=2.15.0", want: true},
		{name: "floor equal to the controller", release: ">=2.16.0", want: true},
		{name: "floor above the controller", release: ">=2.18.0", want: false},
		{name: "floor inside the controller range", release: ">=2.16.5", want: false},
		{name: "ceiling below the controller ceiling", release: ">=2.9,<2.11", want: false},
		{name: "ceiling at the controller ceiling", release: ">=2.9,<2.17.0", want: true},
		{name: "ceiling above the controller ceiling", release: ">=2.9,<2.19.0", want: true},
		{name: "exclusive floor at an inclusive controller floor", release: ">2.16.0", want: false},
	}

	controller := mustRange(t, controllerSpec)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release := mustRange(t, tt.release)
			if got := release.admits(controller); got != tt.want {
				t.Errorf("(%s).admits(%s) = %t, want %t", release, controller, got, tt.want)
			}
		})
	}
}

// galaxyFixture mirrors what the Galaxy v3 versions endpoint actually serves for
// community.general, including the requires_ansible shapes seen across its history.
func galaxyFixture() []galaxyVersion {
	return []galaxyVersion{
		{Version: "13.4.0", RequiresAnsible: ">=2.18.0"},
		{Version: "12.6.5", RequiresAnsible: ">=2.17.0"},
		{Version: "11.4.9", RequiresAnsible: ">=2.16.0"},
		{Version: "11.4.8", RequiresAnsible: ">=2.16.0"},
		{Version: "10.7.2", RequiresAnsible: ">=2.15.0"},
		{Version: "9.5.9", RequiresAnsible: ">=2.13.0"},
	}
}

func TestResolveCollection(t *testing.T) {
	controller := mustRange(t, ">=2.16.0,<2.17.0")

	best, newest, err := resolveCollection(galaxyFixture(), controller)
	if err != nil {
		t.Fatalf("resolveCollection: %v", err)
	}
	if best.Version != "11.4.9" {
		t.Errorf("best = %s, want 11.4.9", best.Version)
	}
	// The newest release is reported even though it is unusable: the gap between the two is
	// what tells a reader the pin is held back rather than current.
	if newest.Version != "13.4.0" {
		t.Errorf("newest = %s, want 13.4.0", newest.Version)
	}
}

// TestResolveCollectionSkipsPrereleases keeps requirements.yml to what a release installs.
func TestResolveCollectionSkipsPrereleases(t *testing.T) {
	versions := append(galaxyFixture(), galaxyVersion{Version: "11.5.0-a1", RequiresAnsible: ">=2.16.0"})

	best, _, err := resolveCollection(versions, mustRange(t, ">=2.16.0,<2.17.0"))
	if err != nil {
		t.Fatalf("resolveCollection: %v", err)
	}
	if best.Version != "11.4.9" {
		t.Errorf("best = %s, want 11.4.9: a prerelease must never win the pin", best.Version)
	}
}

// TestResolveCollectionAdmitsAnAbsentSpecifier covers versions published before Galaxy required
// requires_ansible, which the endpoint serves as null.
func TestResolveCollectionAdmitsAnAbsentSpecifier(t *testing.T) {
	versions := []galaxyVersion{
		{Version: "2.0.0", RequiresAnsible: ">=2.18.0"},
		{Version: "1.6.1", RequiresAnsible: ""},
	}

	best, newest, err := resolveCollection(versions, mustRange(t, ">=2.16.0,<2.17.0"))
	if err != nil {
		t.Fatalf("resolveCollection: %v", err)
	}
	if best.Version != "1.6.1" {
		t.Errorf("best = %s, want 1.6.1", best.Version)
	}
	if newest.Version != "2.0.0" {
		t.Errorf("newest = %s, want 2.0.0", newest.Version)
	}
}

// TestResolveCollectionSkipsAMalformedSpecifier keeps an upstream publisher's bad metadata from
// being read as "unconstrained", which would pin a release that cannot run.
func TestResolveCollectionSkipsAMalformedSpecifier(t *testing.T) {
	versions := []galaxyVersion{
		{Version: "12.0.0", RequiresAnsible: "2.18.0"},
		{Version: "11.4.9", RequiresAnsible: ">=2.16.0"},
	}

	best, _, err := resolveCollection(versions, mustRange(t, ">=2.16.0,<2.17.0"))
	if err != nil {
		t.Fatalf("resolveCollection: %v", err)
	}
	if best.Version != "11.4.9" {
		t.Errorf("best = %s, want 11.4.9", best.Version)
	}
}

func TestResolveCollectionWhenNothingSupportsTheController(t *testing.T) {
	versions := []galaxyVersion{{Version: "13.4.0", RequiresAnsible: ">=2.18.0"}}

	_, newest, err := resolveCollection(versions, mustRange(t, ">=2.16.0,<2.17.0"))
	if err == nil {
		t.Fatal("resolveCollection: got nil error, want a failure")
	}
	// The newest release is still returned, so the caller can name what it found.
	if newest.Version != "13.4.0" {
		t.Errorf("newest = %s, want 13.4.0", newest.Version)
	}
}

func TestGalaxyVersionsURL(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		coll    string
		want    string
		wantErr bool
	}{
		{
			name:   "galaxy",
			source: "https://galaxy.ansible.com",
			coll:   "community.general",
			want:   "https://galaxy.ansible.com/api/v3/plugin/ansible/content/published/collections/index/community/general/versions/?limit=100",
		},
		{
			name:   "trailing slash on the source",
			source: "https://galaxy.ansible.com/",
			coll:   "ansible.posix",
			want:   "https://galaxy.ansible.com/api/v3/plugin/ansible/content/published/collections/index/ansible/posix/versions/?limit=100",
		},
		{
			name:   "private automation hub under a path",
			source: "https://hub.example.mil/api/galaxy",
			coll:   "kubernetes.core",
			want:   "https://hub.example.mil/api/galaxy/api/v3/plugin/ansible/content/published/collections/index/kubernetes/core/versions/?limit=100",
		},
		{name: "collection without a namespace", source: "https://galaxy.ansible.com", coll: "general", wantErr: true},
		{name: "collection with an empty namespace", source: "https://galaxy.ansible.com", coll: ".general", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := galaxyVersionsURL(tt.source, tt.coll)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("galaxyVersionsURL(%q, %q): got %s, want an error", tt.source, tt.coll, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("galaxyVersionsURL(%q, %q): %v", tt.source, tt.coll, err)
			}
			if got != tt.want {
				t.Errorf("galaxyVersionsURL(%q, %q) = %s, want %s", tt.source, tt.coll, got, tt.want)
			}
		})
	}
}

// TestControllerRangeIsTheCheckedInRange reads the collection's own meta/runtime.yml, so the
// value that every pin is resolved against cannot regress to something unparseable or unbounded
// without a test failing.
func TestControllerRangeIsTheCheckedInRange(t *testing.T) {
	t.Chdir("..")

	got, err := controllerRange()
	if err != nil {
		t.Fatalf("controllerRange: %v", err)
	}
	if got.lo == nil || got.hi == nil {
		t.Fatalf("controllerRange = %s, want a range bounded on both sides", got)
	}
}

// TestCheckedInPinsParse reads the checked-in requirements.yml, so a hand edit that breaks its
// shape fails here rather than in the container build.
func TestCheckedInPinsParse(t *testing.T) {
	t.Chdir("..")

	req, err := readAnsibleRequirements()
	if err != nil {
		t.Fatalf("readAnsibleRequirements: %v", err)
	}

	for _, c := range req.Collections {
		if _, err := galaxyVersionsURL(c.Source, c.Name); err != nil {
			t.Errorf("%s: %v", c.Name, err)
		}
		if c.Version == "" {
			t.Errorf("%s: no version pinned", c.Name)
		}
	}
}
