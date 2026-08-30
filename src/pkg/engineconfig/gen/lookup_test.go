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
