// Copyright 2021 zarf authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from zarf:
// https://github.com/zarf-dev/zarf
//
// Modifications Copyright 2026 colonel-byte.
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

package cfg

import (
	"context"
	"testing"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/stretchr/testify/require"
)

// newer is a future apiVersion this binary does not understand.
const newer = "zarf.dev/v1beta999"

func TestParseDefinition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		yaml     string
		wantName string
		wantErr  string
	}{
		{
			name: "omitted apiVersion parses as v1alpha1",
			yaml: `
kind: Distro
metadata:
  name: no-api-version
`,
			wantName: "no-api-version",
		},
		{
			name: "explicit v1alpha1 apiVersion parses",
			yaml: `
apiVersion: colonel-byte.dev/v1alpha1
kind: Distro
metadata:
  name: explicit-v1alpha1
`,
			wantName: "explicit-v1alpha1",
		},
		{
			name: "unknown apiVersion errors without silent fallback",
			yaml: `
apiVersion: ` + newer + `
kind: Distro
metadata:
  name: from-future
`,
			wantErr: `unsupported apiVersion "` + newer + `"`,
		},
		{
			name: "multi-document input errors",
			yaml: `
apiVersion: colonel-byte.dev/v1alpha1
kind: Distro
metadata:
  name: first
---
apiVersion: colonel-byte.dev/v1alpha1
kind: Distro
metadata:
  name: second
`,
			wantErr: "single YAML document",
		},
		{
			name: "leading document separator is accepted",
			yaml: `---
apiVersion: colonel-byte.dev/v1alpha1
kind: Distro
metadata:
  name: leading-sep
`,
			wantName: "leading-sep",
		},
		{
			name: "leading and trailing separators are accepted",
			yaml: `---
apiVersion: colonel-byte.dev/v1alpha1
kind: Distro
metadata:
  name: both-sep
---
`,
			wantName: "both-sep",
		},
		{
			name:    "empty input errors",
			yaml:    "",
			wantErr: "no package definition found",
		},
		{
			name:    "whitespace-only input errors",
			yaml:    "\n  \n",
			wantErr: "no package definition found",
		},
		{
			name:    "malformed yaml bubbles up from the parser",
			yaml:    "apiVersion: [not, a, string]\n",
			wantErr: "apiVersion",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pkg, err := Parse(context.Background(), []byte(tt.yaml))
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				require.Equal(t, distro.Distro{}, pkg)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantName, pkg.Metadata.Name)
		})
	}
}

func TestParseBuiltPackageDefinition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		yaml     string
		wantName string
		wantErr  string
	}{
		{
			name: "single v1alpha1 doc parses",
			yaml: `
apiVersion: colonel-byte.dev/v1alpha1
kind: Distro
metadata:
  name: single
`,
			wantName: "single",
		},
		{
			name: "picks v1alpha1 when newer doc is unrecognized",
			yaml: `
apiVersion: colonel-byte.dev/v1alpha1
kind: Distro
metadata:
  name: from-v1alpha1
---
apiVersion: ` + newer + `
kind: Distro
metadata:
  name: from-future
`,
			wantName: "from-v1alpha1",
		},
		{
			name: "tolerates reverse order",
			yaml: `
apiVersion: ` + newer + `
kind: Distro
metadata:
  name: from-future
---
apiVersion: colonel-byte.dev/v1alpha1
kind: Distro
metadata:
  name: from-v1alpha1
`,
			wantName: "from-v1alpha1",
		},
		{
			name: "errors when no known version present",
			yaml: `
apiVersion: ` + newer + `
kind: Distro
metadata:
  name: from-future
`,
			wantErr: "no supported apiVersion found",
		},
		{
			name: "errors on duplicate same-version docs",
			yaml: `
apiVersion: colonel-byte.dev/v1alpha1
kind: Distro
metadata:
  name: first
---
apiVersion: colonel-byte.dev/v1alpha1
kind: Distro
metadata:
  name: second
`,
			wantErr: `duplicate apiVersion "colonel-byte.dev/v1alpha1"`,
		},
		{
			name: "trailing document separator is ignored",
			yaml: `
apiVersion: colonel-byte.dev/v1alpha1
kind: Distro
metadata:
  name: trailing
---
`,
			wantName: "trailing",
		},
		{
			name:    "empty input errors",
			yaml:    "",
			wantErr: "no package definition found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pkg, err := ParseMultiDoc(context.Background(), []byte(tt.yaml))
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				require.Equal(t, distro.Distro{}, pkg)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantName, pkg.Metadata.Name)
		})
	}
}

func TestHandlerFor(t *testing.T) {
	t.Parallel()

	// Empty apiVersion and explicit v1alpha1 must resolve to the same handler.
	emptyHandler, emptyOK := handlerFor("")
	require.True(t, emptyOK)
	v1Handler, v1OK := handlerFor(v1alpha1.APIVersion)
	require.True(t, v1OK)
	require.Equal(t, v1Handler.version, emptyHandler.version)
	require.Equal(t, v1Handler.priority, emptyHandler.priority)

	_, unknownOK := handlerFor("zarf.dev/v1beta999")
	require.False(t, unknownOK)

	// Duplicate priorities would make "latest" ambiguous.
	priorities := map[int]string{}
	for _, h := range knownAPIVersions {
		if existing, dup := priorities[h.priority]; dup {
			t.Fatalf("duplicate priority %d shared by %q and %q", h.priority, existing, h.version)
		}
		priorities[h.priority] = h.version
	}
}

// TestDeprecatedAPIVersionStillParses pins the transition window: a distro package written
// against the zarf.dev group keeps parsing after the move to colonel-byte.dev. The decode is
// shared, so the only thing that can break this is the handler table losing its entry.
func TestDeprecatedAPIVersionStillParses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		yaml     string
		wantName string
	}{
		{
			name: "deprecated group and kind",
			yaml: `
apiVersion: zarf.dev/v1alpha1
kind: ZarfDistro
metadata:
  name: legacy
`,
			wantName: "legacy",
		},
		{
			name: "current group and kind",
			yaml: `
apiVersion: colonel-byte.dev/v1alpha1
kind: Distro
metadata:
  name: current
`,
			wantName: "current",
		},
		{
			name: "deprecated group with the new kind",
			yaml: `
apiVersion: zarf.dev/v1alpha1
kind: Distro
metadata:
  name: mixed
`,
			wantName: "mixed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pkg, err := Parse(context.Background(), []byte(tt.yaml))
			require.NoError(t, err)
			require.Equal(t, tt.wantName, pkg.Metadata.Name)
		})
	}
}

// TestMultiDocPrefersCurrentAPIVersion pins the priority ordering. A package carrying one
// document per group must be read as the colonel-byte.dev one, because that is the newer
// spelling and priority is what ParseMultiDoc sorts on.
func TestMultiDocPrefersCurrentAPIVersion(t *testing.T) {
	t.Parallel()

	const doc = `
apiVersion: zarf.dev/v1alpha1
kind: ZarfDistro
metadata:
  name: deprecated
---
apiVersion: colonel-byte.dev/v1alpha1
kind: Distro
metadata:
  name: current
`

	pkg, err := ParseMultiDoc(context.Background(), []byte(doc))
	require.NoError(t, err)
	require.Equal(t, "current", pkg.Metadata.Name)
}
