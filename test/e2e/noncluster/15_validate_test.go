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

package noncluster

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCargoshipValidate exercises the validate command against the real binary. The schema comes
// out of the binary, so like the schema command every case here runs without a network.
func TestCargoshipValidate(t *testing.T) {
	t.Run("accepts every inventory the tests ship", func(t *testing.T) {
		// These are the files the rest of the suite installs from, in every spelling YAML
		// allows -- anchors, flow style, merge keys. A false positive here would make the
		// command useless, so they are checked as a group.
		inventories, err := filepath.Glob(filepath.Join("test", "e2e", "noncluster", "testdata", "inventory*.yaml"))
		require.NoError(t, err)
		require.NotEmpty(t, inventories)

		args := append([]string{"validate"}, inventories...)
		_, stderr, err := e2e.Cargoship(t, args...)
		require.NoError(t, err, stderr)
		require.Contains(t, stderr, "ok (inventory)")
	})

	t.Run("accepts a generated example and the repository config", func(t *testing.T) {
		// The kind is read from each document, so a package definition and an inventory can be
		// checked in one run. The config file declares no kind and needs --kind.
		_, stderr, err := e2e.Cargoship(t, "validate", filepath.Join(valuesSchemaDistroDir, "distro.yaml"))
		require.NoError(t, err, stderr)
		require.Contains(t, stderr, "ok (package)")

		_, stderr, err = e2e.Cargoship(t, "validate", "--kind", "config", "cargoship-config.yaml")
		require.NoError(t, err, stderr)
		require.Contains(t, stderr, "ok (config)")
	})

	t.Run("reports every problem in one pass", func(t *testing.T) {
		// The gap this closes: cargoship unmarshals an inventory without strict key checking, so
		// this file parses cleanly today and installs with no load balancer address.
		path := filepath.Join(t.TempDir(), "inventory.yaml")
		require.NoError(t, os.WriteFile(path, []byte(`
apiVersion: zarf.dev/v1alpha1
kind: ZarfCluster
metadata:
  name: demo
spec:
  config:
    loadbalancr: lb.example.com
  hosts:
    - hostname: node1
      role: controler
`), 0600))

		_, stderr, err := e2e.Cargoship(t, "validate", path)
		require.Error(t, err)
		require.Contains(t, stderr, "Additional property loadbalancr is not allowed")
		require.Contains(t, stderr, "loadbalancer is required")
		require.Contains(t, stderr, `must be one of the following: "controller", "worker"`)
	})

	t.Run("needs a kind for a document that declares none", func(t *testing.T) {
		_, stderr, err := e2e.Cargoship(t, "validate", "cargoship-config.yaml")
		require.Error(t, err)
		require.Contains(t, stderr, "declares no kind")
	})

	t.Run("checks spec.config.values against a built package", func(t *testing.T) {
		outDir := t.TempDir()
		_, _, err := e2e.Cargoship(t, "create", valuesSchemaDistroDir, "-o", outDir)
		require.NoError(t, err)

		matches, err := filepath.Glob(filepath.Join(outDir, "*.tar.zst"))
		require.NoError(t, err)
		require.Len(t, matches, 1)

		path := filepath.Join(t.TempDir(), "inventory.yaml")
		require.NoError(t, os.WriteFile(path, []byte(`
apiVersion: zarf.dev/v1alpha1
kind: ZarfCluster
metadata:
  name: demo
spec:
  config:
    loadbalancer: lb.example.com
    values:
      replicaz: 3
  hosts:
    - hostname: node1
      role: controller
`), 0600))

		// Untyped without a package, so the typo passes and is only caught at install time.
		_, stderr, err := e2e.Cargoship(t, "validate", path)
		require.NoError(t, err, stderr)

		_, stderr, err = e2e.Cargoship(t, "validate", path, "--package", matches[0])
		require.Error(t, err)
		require.Contains(t, stderr, "Additional property replicaz is not allowed")
	})

	t.Run("rejects --package for a document that is not an inventory", func(t *testing.T) {
		_, stderr, err := e2e.Cargoship(t, "validate",
			filepath.Join(valuesSchemaDistroDir, "distro.yaml"), "--package", valuesSchemaDistroDir)
		require.Error(t, err)
		require.Contains(t, stderr, "only applies to the inventory schema")
	})
}
