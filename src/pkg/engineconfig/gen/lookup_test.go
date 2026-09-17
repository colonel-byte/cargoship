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

package gen

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLookupMatchesOnMinorVersionRegardlessOfPatchOrFormat(t *testing.T) {
	cases := []string{
		"1.35.3-k3s1",
		"v1.35.3-k3s1",
		"1.35.0+k3s1",
		"v1.35.99+k3s1",
	}
	for _, version := range cases {
		entry, ok := Lookup("k3s", version)
		require.True(t, ok, version)
		require.NotNil(t, entry.Server)
		require.NotNil(t, entry.Agent)
	}
}

func TestLookupUnknownDistroOrVersion(t *testing.T) {
	_, ok := Lookup("k0s", "1.35.3")
	require.False(t, ok)

	_, ok = Lookup("k3s", "9.99.99")
	require.False(t, ok)

	_, ok = Lookup("k3s", "not-a-version")
	require.False(t, ok)
}

func TestKeysReadsYAMLTagNames(t *testing.T) {
	entry, ok := Lookup("k3s", "1.35.3-k3s1")
	require.True(t, ok)

	keys := Keys(entry.Server)
	require.Contains(t, keys, "cluster-cidr")
	require.Contains(t, keys, "node-name")
	require.NotContains(t, keys, "cluster-cidr,omitempty") // tag suffix must be stripped
}

func TestKeysNonStructReturnsEmpty(t *testing.T) {
	require.Empty(t, Keys(nil))
	require.Empty(t, Keys("not a struct"))
}

func TestUnknownAddonsListShape(t *testing.T) {
	addons := []string{"rke2-coredns", "rke2-ingress-nginx"}
	unknown := UnknownAddons([]any{"rke2-ingress-nginx", "rke2-typo", "rke2-coredns"}, addons)
	require.Equal(t, []string{"rke2-typo"}, unknown)
}

func TestUnknownAddonsScalarAndStringSlice(t *testing.T) {
	addons := []string{"coredns", "traefik"}
	require.Empty(t, UnknownAddons("traefik", addons))
	require.Equal(t, []string{"nope"}, UnknownAddons("nope", addons))
	require.Equal(t, []string{"nope"}, UnknownAddons([]string{"coredns", "nope"}, addons))
}

// A version whose component source was never pulled has no vocabulary, so nothing is reported.
func TestUnknownAddonsNoVocabulary(t *testing.T) {
	require.Nil(t, UnknownAddons([]any{"anything"}, nil))
}

func TestUnknownAddonsAbsentOrOddValue(t *testing.T) {
	addons := []string{"coredns"}
	require.Empty(t, UnknownAddons(nil, addons))
	require.Empty(t, UnknownAddons(42, addons))
	require.Equal(t, []string{"nope"}, UnknownAddons([]any{"nope", 42, nil}, addons))
}

func TestUnknownAddonsDeduplicates(t *testing.T) {
	require.Equal(t, []string{"nope"}, UnknownAddons([]any{"nope", "nope"}, []string{"coredns"}))
}

// The registry the generator wrote must actually carry the addon vocabulary, or every consumer
// silently skips the check.
func TestRegistryCarriesAddons(t *testing.T) {
	k3s, ok := Lookup("k3s", "v1.37.0+k3s1")
	require.True(t, ok)
	require.Contains(t, k3s.Addons, "traefik")
	require.Empty(t, k3s.CNIs)

	rke2, ok := Lookup("rke2", "v1.37.0+rke2r1")
	require.True(t, ok)
	require.Contains(t, rke2.Addons, "rke2-coredns")
	// Not in rke2's own DisableItems -- only reachable through the CNI/ingress chart union.
	require.Contains(t, rke2.Addons, "rke2-ingress-nginx")
	require.Contains(t, rke2.Addons, "rke2-multus-crd")
	require.Contains(t, rke2.CNIs, "cilium")
	require.Contains(t, rke2.IngressControllers, "traefik")
}
