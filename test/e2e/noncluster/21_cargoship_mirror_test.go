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
	"fmt"
	"testing"

	"github.com/colonel-byte/cargoship/test"
	ggcrname "github.com/google/go-containerregistry/pkg/name"
	ggcrremote "github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/stretchr/testify/require"
)

// TestCargoshipMirrorImages exercises the built binary's "mirror" command against an
// in-memory OCI registry.
//
// What it is really asserting is the reference each image lands under. A mirror whose
// contents are correct but whose names are not is indistinguishable from an empty one as far
// as a node is concerned: cargoship writes the mirror's address into each host's containerd
// hosts.toml, and containerd then asks for the repository path unchanged. So the path and
// tag surviving the host swap is the behaviour, not an implementation detail -- see
// docs/guides/mirror-registry.md.
//
// Two registries, not one. The source and the destination have to be different hosts, because
// zarf's transform.ImageTransformHostWithoutChecksum treats a target whose string starts with
// the image's own host as "already transformed" and returns the source reference unchanged --
// so mirroring a package into a prefix on the registry it was built from pushes the images
// straight back where they came from. Reusing one server here would assert that behaviour
// rather than the host swap.
func TestCargoshipMirrorImages(t *testing.T) {
	src := test.SetupInMemoryRegistry(t)
	pushTestImage(t, src, pauseImageTag)
	pushTestImage(t, src, ciliumImageTag)
	pkgPath := imagePackage(t, src)

	addr := test.SetupInMemoryRegistry(t)
	dst := fmt.Sprintf("oci://%s/e2e-mirror", addr)
	_, _, err := e2e.Cargoship(t, "mirror", pkgPath, dst, "--images", "--plain-http")
	require.NoError(t, err)

	// Both images, declared the two different ways the fixture declares them: pause in the
	// flat image list, cilium under a chart entry. A chart's images dropping out of the
	// mirror walk is the failure this catches.
	for _, tag := range []string{pauseImageTag, ciliumImageTag} {
		t.Run(tag, func(t *testing.T) {
			ref, err := ggcrname.ParseReference(fmt.Sprintf("%s/e2e-mirror/%s", addr, tag))
			require.NoError(t, err)

			img, err := ggcrremote.Image(ref)
			require.NoError(t, err, "the mirror holds no %s: the host swap did not keep the path and tag", tag)
			_, err = img.Manifest()
			require.NoError(t, err)
		})
	}
}

// TestCargoshipMirrorContextNeedsZarfState checks that --context is refused rather than
// ignored when nothing is reading a cluster. A silently dropped context looks like it was
// honoured, which is the worse failure: the push goes to the right registry for the wrong
// reason, and the next one does not.
func TestCargoshipMirrorContextNeedsZarfState(t *testing.T) {
	_, stderr, err := e2e.Cargoship(t, "mirror", minimalPackage(t), "oci://127.0.0.1:1/x", "--context", "nonexistent")
	require.Error(t, err)
	require.Contains(t, stderr, "--from-zarf-state")
}
