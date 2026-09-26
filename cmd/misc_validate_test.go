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

package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	pkgschema "github.com/colonel-byte/cargoship/pkg/schema"
	"github.com/stretchr/testify/require"
)

// runValidate builds the command the way the root does and runs it. Everything it says goes to
// stderr, so that a check in a pipeline writes nothing to stdout.
func runValidate(t *testing.T, args ...string) (stdout string, stderr string, err error) {
	t.Helper()

	cmd := newValidateCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err = cmd.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

// writeYAML puts a document on disk under a name the test can recognize in the output.
func writeYAML(t *testing.T, name, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0600))
	return path
}

// valuesSchemaPackageDir is an unbuilt package definition carrying a values schema, read straight
// off disk so these tests need no build and no network.
var valuesSchemaPackageDir = filepath.Join("..", "test", "e2e", "noncluster", "testdata", "values-schema")

const validInventory = `
apiVersion: zarf.dev/v1alpha1
kind: ZarfCluster
metadata:
  name: demo
spec:
  config:
    loadbalancer: lb.example.com
  hosts:
    - hostname: node1
      role: controller
`

const invalidInventory = `
apiVersion: zarf.dev/v1alpha1
kind: ZarfCluster
metadata:
  name: demo
spec:
  config:
    loadbalancr: lb.example.com
  hosts:
    - hostname: node1
      role: controller
`

func TestValidateAcceptsAGoodInventory(t *testing.T) {
	path := writeYAML(t, "inventory.yaml", validInventory)

	stdout, stderr, err := runValidate(t, path)
	require.NoError(t, err)
	require.Empty(t, stdout, "stdout stays clear so the command composes in a pipeline")
	require.Contains(t, stderr, "ok (inventory)")
}

func TestValidateDetectsThePackageKind(t *testing.T) {
	path := writeYAML(t, "distro.yaml", `
apiVersion: zarf.dev/v1alpha1
kind: ZarfDistro
metadata:
  name: demo
  version: 0.0.1
spec:
  type: rke2
  version: 1.35.0-rke2r1
  config:
    engine: {}
`)

	_, stderr, err := runValidate(t, path)
	require.NoError(t, err)
	require.Contains(t, stderr, "ok (package)")
}

func TestValidateReportsEveryProblem(t *testing.T) {
	path := writeYAML(t, "inventory.yaml", invalidInventory)

	_, _, err := runValidate(t, path)
	var invalid *pkgschema.ValidationError
	require.ErrorAs(t, err, &invalid)
	require.Contains(t, err.Error(), "Additional property loadbalancr is not allowed")
	require.Contains(t, err.Error(), "loadbalancer is required")
}

func TestValidateChecksEveryFileBeforeReporting(t *testing.T) {
	// One run should be enough to fix a directory, so a failure early in the list does not stop
	// the files after it from being checked.
	dir := t.TempDir()
	bad1 := filepath.Join(dir, "one.yaml")
	good := filepath.Join(dir, "two.yaml")
	bad2 := filepath.Join(dir, "three.yaml")
	require.NoError(t, os.WriteFile(bad1, []byte(invalidInventory), 0600))
	require.NoError(t, os.WriteFile(good, []byte(validInventory), 0600))
	require.NoError(t, os.WriteFile(bad2, []byte("kind: ZarfCluster\nspec:\n  hosts: {}\n"), 0600))

	_, stderr, err := runValidate(t, bad1, good, bad2)
	require.Error(t, err)
	require.Contains(t, err.Error(), "2 of 3 files do not match their schema")
	require.Contains(t, err.Error(), "one.yaml")
	require.Contains(t, err.Error(), "three.yaml")
	require.Contains(t, stderr, "two.yaml: ok")

	// The individual failures survive the aggregate, so a caller can still reach one.
	var invalid *pkgschema.ValidationError
	require.ErrorAs(t, err, &invalid)
}

func TestValidateNeedsAKindForAConfigFile(t *testing.T) {
	path := writeYAML(t, "cargoship-config.yaml", "architecture: amd64\n")

	_, _, err := runValidate(t, path)
	require.ErrorContains(t, err, "declares no kind")
	require.ErrorContains(t, err, "--kind config|inventory|package")

	_, stderr, err := runValidate(t, "--kind", "config", path)
	require.NoError(t, err)
	require.Contains(t, stderr, "ok (config)")
}

func TestValidateKindOverridesTheDocument(t *testing.T) {
	// --kind wins, so a document whose kind cargoship does not recognize can still be checked
	// deliberately.
	path := writeYAML(t, "inventory.yaml", validInventory)

	_, stderr, err := runValidate(t, "--kind", "inventory", path)
	require.NoError(t, err)
	require.Contains(t, stderr, "ok (inventory)")
}

func TestValidateRejectsAnUnknownKind(t *testing.T) {
	path := writeYAML(t, "inventory.yaml", validInventory)

	_, _, err := runValidate(t, "--kind", "cluster", path)
	require.ErrorContains(t, err, "unknown schema")
}

func TestValidateRejectsPackageFlagForOtherKinds(t *testing.T) {
	path := writeYAML(t, "cargoship-config.yaml", "architecture: amd64\n")

	_, _, err := runValidate(t, "--kind", "config", "--package", "./nowhere", path)
	require.ErrorContains(t, err, "only applies to the inventory schema")
}

func TestValidateRejectsPackageFlagForADetectedPackageDocument(t *testing.T) {
	// --kind was not given, so the mismatch is only visible once the document says what it is.
	path := writeYAML(t, "distro.yaml", "kind: ZarfDistro\n")

	_, _, err := runValidate(t, "--package", valuesSchemaPackageDir, path)
	require.ErrorContains(t, err, "only applies to the inventory schema")
}

func TestValidateChecksValuesAgainstAPackage(t *testing.T) {
	// spec.config.values is untyped in the generated schema because its shape belongs to the
	// package. Without --package the typo below passes and is only caught at install time.
	path := writeYAML(t, "inventory.yaml", `
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
`)

	_, _, err := runValidate(t, path)
	require.NoError(t, err)

	_, _, err = runValidate(t, "--package", valuesSchemaPackageDir, path)
	require.ErrorContains(t, err, "spec.config.values: Additional property replicaz is not allowed")
}

func TestValidateReportsAMissingFile(t *testing.T) {
	// A problem with the run rather than with a document, so it stops rather than being collected
	// alongside schema failures.
	_, _, err := runValidate(t, filepath.Join(t.TempDir(), "absent.yaml"))
	require.ErrorContains(t, err, "unable to read")
}

func TestValidateReportsUnparseableYAML(t *testing.T) {
	path := writeYAML(t, "inventory.yaml", "hosts:\n  - name: a\n   role: b\n")

	_, _, err := runValidate(t, path)
	require.ErrorContains(t, err, "unable to parse YAML")
}
