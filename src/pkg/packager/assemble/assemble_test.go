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

package assemble

import (
	"reflect"
	"testing"
	"time"

	"github.com/colonel-byte/cargoship/src/api"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/pkg/images"
)

func TestBuildTimestampReproducible(t *testing.T) {
	got := buildTimestamp(true)
	if !got.Equal(config.InitCommit) {
		t.Fatalf("buildTimestamp(true) = %v, want %v", got, config.InitCommit)
	}
}

func TestBuildTimestampNonReproducible(t *testing.T) {
	before := time.Now()
	got := buildTimestamp(false)
	after := time.Now()

	if got.Before(before) || got.After(after) {
		t.Fatalf("buildTimestamp(false) = %v, want between %v and %v", got, before, after)
	}
	if got.Equal(config.InitCommit) {
		t.Fatalf("buildTimestamp(false) unexpectedly equals config.InitCommit")
	}
}

func TestRecordDistroMetadataReproducible(t *testing.T) {
	d := distro.ZarfDistro{}
	d.Metadata.Architecture = "amd64"
	d.Metadata.Version = "1.2.3"

	opts := AssembleOptions{
		Reproducible: true,
		RegistryOverrides: []images.RegistryOverride{
			{Source: "docker.io", Override: "registry.example.com"},
		},
	}

	got := recordDistroMetadata(d, opts)

	if got.Build.Architecture != "amd64" {
		t.Errorf("Build.Architecture = %q, want %q", got.Build.Architecture, "amd64")
	}
	if got.Build.Version != "1.2.3" {
		t.Errorf("Build.Version = %q, want %q", got.Build.Version, "1.2.3")
	}
	if !got.Build.Reproducible {
		t.Errorf("Build.Reproducible = false, want true")
	}
	wantTimestamp := config.InitCommit.Format(api.BuildTimestampFormat)
	if got.Build.Timestamp != wantTimestamp {
		t.Errorf("Build.Timestamp = %q, want %q", got.Build.Timestamp, wantTimestamp)
	}
	if want := "registry.example.com"; got.Build.RegistryOverrides["docker.io"] != want {
		t.Errorf("Build.RegistryOverrides[docker.io] = %q, want %q", got.Build.RegistryOverrides["docker.io"], want)
	}
}

func TestRecordDistroMetadataNonReproducible(t *testing.T) {
	d := distro.ZarfDistro{}
	d.Metadata.Architecture = "arm64"
	d.Metadata.Version = "4.5.6"

	before := time.Now()
	got := recordDistroMetadata(d, AssembleOptions{Reproducible: false})
	after := time.Now()

	if got.Build.Reproducible {
		t.Errorf("Build.Reproducible = true, want false")
	}
	parsed, err := time.Parse(api.BuildTimestampFormat, got.Build.Timestamp)
	if err != nil {
		t.Fatalf("Build.Timestamp %q did not parse as %s: %v", got.Build.Timestamp, api.BuildTimestampFormat, err)
	}
	if parsed.Before(before.Truncate(time.Second)) || parsed.After(after) {
		t.Errorf("Build.Timestamp %v not between %v and %v", parsed, before, after)
	}
}

// TestReproducibleAssemblyIsDeterministic runs the same reproducibility guarantee
// AssembleDistro relies on at the unit level: two calls to recordDistroMetadata with
// Reproducible: true, for otherwise-identical input, must agree on every recorded
// field -- there's nothing time- or machine-dependent left once Reproducible is set.
func TestReproducibleAssemblyIsDeterministic(t *testing.T) {
	d := distro.ZarfDistro{}
	d.Metadata.Architecture = "amd64"
	d.Metadata.Version = "1.0.0"
	opts := AssembleOptions{Reproducible: true}

	first := recordDistroMetadata(d, opts)
	time.Sleep(10 * time.Millisecond)
	second := recordDistroMetadata(d, opts)

	if !reflect.DeepEqual(first.Build, second.Build) {
		t.Fatalf("recordDistroMetadata not deterministic under Reproducible: true:\nfirst:  %+v\nsecond: %+v", first.Build, second.Build)
	}
}
