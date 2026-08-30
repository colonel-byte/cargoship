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

// Package test provides e2e tests for cargoship
package test

import (
	"encoding/json"
	"strings"
	"testing"

	goyaml "github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

// versionOutput mirrors the structure `version -o json|yaml` prints.
type versionOutput struct {
	Build        map[string]string `json:"build" yaml:"build"`
	Dependencies map[string]string `json:"dependencies" yaml:"dependencies"`
}

// TestCargoshipVersion exercises the `version` command's plain output, its `v` alias, both
// structured output formats, and the unsupported-format error path.
func TestCargoshipVersion(t *testing.T) {
	t.Run("prints the CLI version", func(t *testing.T) {
		stdout, _, err := e2e.Cargoship(t, "version")
		require.NoError(t, err)
		require.NotEmpty(t, strings.TrimSpace(stdout))
	})

	t.Run("v alias prints the same version", func(t *testing.T) {
		want, _, err := e2e.Cargoship(t, "version")
		require.NoError(t, err)

		stdout, _, err := e2e.Cargoship(t, "v")
		require.NoError(t, err)
		require.Equal(t, strings.TrimSpace(want), strings.TrimSpace(stdout))
	})

	t.Run("json output carries build info and dependencies", func(t *testing.T) {
		stdout, _, err := e2e.Cargoship(t, "version", "-o", "json")
		require.NoError(t, err)

		var got versionOutput
		require.NoError(t, json.Unmarshal([]byte(stdout), &got))
		requireBuildInfo(t, got)
	})

	t.Run("yaml output carries build info and dependencies", func(t *testing.T) {
		stdout, _, err := e2e.Cargoship(t, "version", "--output", "yaml")
		require.NoError(t, err)

		var got versionOutput
		require.NoError(t, goyaml.Unmarshal([]byte(stdout), &got))
		requireBuildInfo(t, got)
	})

	t.Run("unsupported output format errors", func(t *testing.T) {
		_, _, err := e2e.Cargoship(t, "version", "-o", "xml")
		require.Error(t, err)
	})
}

func requireBuildInfo(t *testing.T, got versionOutput) {
	t.Helper()

	for _, key := range []string{"version", "commit", "platform", "go"} {
		require.NotEmpty(t, got.Build[key], "build.%s must be set", key)
	}
	require.NotEmpty(t, got.Dependencies, "dependencies must not be empty")
}
