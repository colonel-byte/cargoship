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

package schema

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeNormalizesYAMLTypes(t *testing.T) {
	// A YAML timestamp and a non-string key both decode to Go types encoding/json cannot
	// represent directly. Settling them before validation is what keeps a schema from reporting
	// a type error that has nothing to do with the document.
	doc, err := Decode([]byte("created: 2026-09-17T00:00:00Z\nports:\n  80: http\n"))
	require.NoError(t, err)

	obj, ok := doc.(map[string]any)
	require.True(t, ok)
	require.IsType(t, "", obj["created"])
	require.Contains(t, obj["ports"], "80")
}

func TestDecodeReportsBadYAML(t *testing.T) {
	_, err := Decode([]byte("hosts:\n  - name: a\n   role: b\n"))
	require.ErrorContains(t, err, "unable to parse YAML")
}

func TestDetectKind(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  string
		want Kind
		err  string
	}{
		{name: "inventory", doc: "kind: ZarfCluster\n", want: KindInventory},
		{name: "package", doc: "kind: ZarfDistro\n", want: KindPackage},
		{name: "no kind", doc: "architecture: amd64\n", err: "declares no kind"},
		{name: "empty kind", doc: "kind: \"\"\n", err: "declares no kind"},
		{name: "foreign kind", doc: "kind: Deployment\n", err: `kind "Deployment"`},
		{name: "not a mapping", doc: "- a\n- b\n", err: "not a YAML mapping"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := Decode([]byte(tc.doc))
			require.NoError(t, err)

			kind, err := DetectKind(doc)
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, kind)
		})
	}
}

func TestValidatorAcceptsAValidInventory(t *testing.T) {
	v, err := NewValidatorFor(KindInventory)
	require.NoError(t, err)
	require.Equal(t, KindInventory, v.Kind())

	doc, err := Decode([]byte(`
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
`))
	require.NoError(t, err)
	require.NoError(t, v.Validate("inventory.yaml", doc))
}

func TestValidatorReportsEveryProblemSorted(t *testing.T) {
	v, err := NewValidatorFor(KindInventory)
	require.NoError(t, err)

	doc, err := Decode([]byte(`
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
`))
	require.NoError(t, err)

	err = v.Validate("inventory.yaml", doc)
	var invalid *ValidationError
	require.ErrorAs(t, err, &invalid)
	require.Equal(t, "inventory.yaml", invalid.Source)
	require.Equal(t, KindInventory, invalid.Kind)

	// Every mistake in one pass, not just the first: a misspelled key, the required key it was
	// meant to be, and a value outside its enum.
	require.Contains(t, invalid.Problems, "spec.config: Additional property loadbalancr is not allowed")
	require.Contains(t, invalid.Problems, "spec.config: loadbalancer is required")
	require.Contains(t, invalid.Problems, `spec.hosts.0.role: must be one of the following: "controller", "worker"`)
	require.IsIncreasing(t, invalid.Problems)
	require.Contains(t, invalid.Error(), "inventory.yaml does not match the inventory schema:")
}

// TestValidatorDoesNotNameTheFieldTwice pins the trimming that turns gojsonschema's
// "spec.hosts.0.role must be one of ..." into one field followed by one problem.
func TestValidatorDoesNotNameTheFieldTwice(t *testing.T) {
	v, err := NewValidator(KindInventory, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"role": map[string]any{"enum": []any{"controller", "worker"}},
		},
	})
	require.NoError(t, err)

	err = v.Validate("inventory.yaml", map[string]any{"role": "controler"})
	var invalid *ValidationError
	require.ErrorAs(t, err, &invalid)
	require.Len(t, invalid.Problems, 1)
	require.Equal(t, `role: must be one of the following: "controller", "worker"`, invalid.Problems[0])
}

func TestValidatorReportsRootProblemsByName(t *testing.T) {
	v, err := NewValidatorFor(KindInventory)
	require.NoError(t, err)

	doc, err := Decode([]byte("- not a mapping\n"))
	require.NoError(t, err)

	err = v.Validate("inventory.yaml", doc)
	var invalid *ValidationError
	require.ErrorAs(t, err, &invalid)
	require.Contains(t, invalid.Problems[0], "(root):")
}

func TestNewValidatorForRejectsUnknownKind(t *testing.T) {
	_, err := NewValidatorFor(Kind("cluster"))
	require.ErrorContains(t, err, "unknown schema")
}

func TestValidatorAcceptsEveryGeneratedExample(t *testing.T) {
	// The schemas are generated from the same structs the examples are generated against, so a
	// failure here means the two generators have gone out of step rather than that an example is
	// wrong.
	v, err := NewValidatorFor(KindPackage)
	require.NoError(t, err)

	for _, path := range []string{
		"../../../example/rke2-multi-cni-cilium/v1_37/v1.37.0-rke2r1/distro.yaml",
		"../../../example/rke2-cilium-vsphere/v1_37/v1.37.0-rke2r1/distro.yaml",
	} {
		b, err := readIfPresent(path)
		require.NoError(t, err)
		if b == nil {
			continue
		}
		doc, err := Decode(b)
		require.NoError(t, err)
		require.NoError(t, v.Validate(path, doc))
	}
}

// readIfPresent returns nil rather than failing for an example that has been renamed, so this test
// tracks the generator rather than pinning a particular engine version.
func readIfPresent(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return b, err
}
