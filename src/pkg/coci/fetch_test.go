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

package coci

import (
	"context"
	"testing"

	"github.com/colonel-byte/cargoship/src/api"
	"github.com/stretchr/testify/require"
)

// TestSoleManifestDigest covers pulling a package the host's own architecture does not match.
// Refusing it would be pointless whenever the package holds only one manifest, because that
// manifest is the only thing a matching platform could have resolved to either.
func TestSoleManifestDigest(t *testing.T) {
	ctx := context.Background()

	t.Run("an index listing one manifest under one architecture", func(t *testing.T) {
		remote := testRemote(t, "0.0.1")
		desc := pushManifest(t, remote, "arm64 package")
		require.NoError(t, remote.updateIndexForArches(ctx, "0.0.1", desc, api.Arches{"arm64"}))

		got, err := remote.SoleManifestDigest(ctx)
		require.NoError(t, err)
		require.Equal(t, desc.Digest, got)
	})

	t.Run("an index listing the same manifest under several architectures", func(t *testing.T) {
		remote := testRemote(t, "0.0.1")
		desc := pushManifest(t, remote, "multi package")
		require.NoError(t, remote.updateIndexForArches(ctx, "0.0.1", desc, api.Arches{"arm64", "s390x"}))

		got, err := remote.SoleManifestDigest(ctx)
		require.NoError(t, err)
		require.Equal(t, desc.Digest, got)
	})

	t.Run("an index listing distinct manifests is left to platform matching", func(t *testing.T) {
		remote := testRemote(t, "0.0.1")
		arm64Desc := pushManifest(t, remote, "arm64 package")
		s390xDesc := pushManifest(t, remote, "s390x package")
		require.NoError(t, remote.updateIndexForArches(ctx, "0.0.1", arm64Desc, api.Arches{"arm64"}))
		require.NoError(t, remote.updateIndexForArches(ctx, "0.0.1", s390xDesc, api.Arches{"s390x"}))

		_, err := remote.SoleManifestDigest(ctx)
		require.ErrorContains(t, err, "lists 2 distinct manifests")
	})

	t.Run("a tag pointing straight at a manifest", func(t *testing.T) {
		remote := testRemote(t, "0.0.1")
		pushManifestTagged(t, remote, "package", "0.0.1")

		_, err := remote.SoleManifestDigest(ctx)
		require.ErrorContains(t, err, "is not an image index")
	})
}
