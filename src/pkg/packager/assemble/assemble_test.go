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
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/colonel-byte/cargoship/src/api"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/pkg/images"
	"github.com/k0sproject/dig"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

func TestBuildTimestampReproducible(t *testing.T) {
	got := buildTimestamp(true)
	if !got.Equal(config.Timestamp) {
		t.Fatalf("buildTimestamp(true) = %v, want %v", got, config.Timestamp)
	}
}

func TestBuildTimestampNonReproducible(t *testing.T) {
	before := time.Now()
	got := buildTimestamp(false)
	after := time.Now()

	if got.Before(before) || got.After(after) {
		t.Fatalf("buildTimestamp(false) = %v, want between %v and %v", got, before, after)
	}
	if got.Equal(config.Timestamp) {
		t.Fatalf("buildTimestamp(false) unexpectedly equals config.Timestamp")
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
	wantTimestamp := config.Timestamp.Format(api.BuildTimestampFormat)
	if got.Build.Timestamp != wantTimestamp {
		t.Errorf("Build.Timestamp = %q, want %q", got.Build.Timestamp, wantTimestamp)
	}
	if want := "registry.example.com"; got.Build.RegistryOverrides["docker.io"] != want {
		t.Errorf("Build.RegistryOverrides[docker.io] = %q, want %q", got.Build.RegistryOverrides["docker.io"], want)
	}
}

func TestRecordDistroMetadataArchitectures(t *testing.T) {
	tests := []struct {
		name         string
		metadata     distro.ZarfDistroMetadata
		wantArches   api.Arches
		wantScalar   api.Arch
		scalarReason string
	}{
		{
			name:         "a scalar architecture is recorded in both fields",
			metadata:     distro.ZarfDistroMetadata{Architecture: api.ArchAMD64},
			wantArches:   api.Arches{api.ArchAMD64},
			wantScalar:   api.ArchAMD64,
			scalarReason: "a single architecture package keeps the scalar older readers look for",
		},
		{
			name:         "a single entry list is recorded in both fields",
			metadata:     distro.ZarfDistroMetadata{Architectures: api.Arches{api.ArchARM64}},
			wantArches:   api.Arches{api.ArchARM64},
			wantScalar:   api.ArchARM64,
			scalarReason: "a single architecture package keeps the scalar older readers look for",
		},
		{
			name:         "several architectures leave the scalar empty",
			metadata:     distro.ZarfDistroMetadata{Architectures: api.Arches{api.ArchAMD64, api.ArchARM64}},
			wantArches:   api.Arches{api.ArchAMD64, api.ArchARM64},
			wantScalar:   "",
			scalarReason: "no single architecture describes what the package carries",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := recordDistroMetadata(distro.ZarfDistro{Metadata: tt.metadata}, AssembleOptions{})

			if !slices.Equal(got.Build.Architectures, tt.wantArches) {
				t.Errorf("Build.Architectures = %v, want %v", got.Build.Architectures, tt.wantArches)
			}
			if got.Build.Architecture != tt.wantScalar {
				t.Errorf("Build.Architecture = %q, want %q: %s", got.Build.Architecture, tt.wantScalar, tt.scalarReason)
			}
		})
	}
}

func TestImageDirForArch(t *testing.T) {
	tests := []struct {
		name      string
		arch      api.Arch
		archCount int
		want      string
	}{
		{
			name:      "a single architecture keeps the flat images directory",
			arch:      api.ArchAMD64,
			archCount: 1,
			want:      filepath.Join("build", "images"),
		},
		{
			name:      "several architectures get one directory each",
			arch:      api.ArchARM64,
			archCount: 2,
			want:      filepath.Join("build", "images", "arm64"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := imageDirForArch("build", tt.arch, tt.archCount); got != tt.want {
				t.Errorf("imageDirForArch() = %q, want %q", got, tt.want)
			}
		})
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

// engineConfigLogContext gives logUnknownEngineConfig a logger whose output can be read
// back, since logging is the whole of what it does.
func engineConfigLogContext() (context.Context, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	l := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return logger.WithContext(context.Background(), l), buf
}

func engineConfigDistro(version string, cfg dig.Mapping) distro.ZarfDistro {
	d := distro.ZarfDistro{}
	d.Spec.Type = "rke2"
	d.Spec.Version = version
	d.Spec.Config.Engine = dig.Mapping{config.EngineConfig: cfg}
	return d
}

func TestLogUnknownEngineConfigLogsOnlyUnknownKeys(t *testing.T) {
	ctx, buf := engineConfigLogContext()

	// cluster-cidr is a server-only key, so it also covers the check reading both roles.
	logUnknownEngineConfig(ctx, engineConfigDistro("1.36.4-rke2r1", dig.Mapping{
		"cluster-cidr":  []string{"10.42.0.0/16"},
		"server":        "https://localhost:9345",
		"totally-typod": "value",
	}))

	out := buf.String()
	if !strings.Contains(out, "key=totally-typod") {
		t.Fatalf("logUnknownEngineConfig did not log the unknown key: %s", out)
	}
	if strings.Contains(out, "key=cluster-cidr") || strings.Contains(out, "key=server") {
		t.Fatalf("logUnknownEngineConfig logged a known key: %s", out)
	}
	if strings.Contains(out, "level=WARN") {
		t.Fatalf("logUnknownEngineConfig logged above debug: %s", out)
	}
}

func TestLogUnknownEngineConfigUnknownVersionLogsNoKeys(t *testing.T) {
	ctx, buf := engineConfigLogContext()

	logUnknownEngineConfig(ctx, engineConfigDistro("1.99.0-rke2r1", dig.Mapping{
		"totally-typod": "value",
	}))

	if out := buf.String(); strings.Contains(out, "key=totally-typod") {
		t.Fatalf("logUnknownEngineConfig flagged a key for a version it has no schema for: %s", out)
	}
}

func TestLogUnknownEngineConfigEmptyConfigLogsNothing(t *testing.T) {
	ctx, buf := engineConfigLogContext()

	logUnknownEngineConfig(ctx, distro.ZarfDistro{})

	if out := buf.String(); out != "" {
		t.Fatalf("logUnknownEngineConfig logged for a distro with no engine config: %s", out)
	}
}
