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
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/colonel-byte/cargoship/pkg/contentstore"
	"github.com/colonel-byte/cargoship/pkg/images"
	"github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
)

// CompositeStore resolves content from several bundles as though they were one.
//
// A zarf package carries a single metadata.architecture by construction, so a build targeting
// two architectures needs one package per architecture. Those two are not alternatives to each
// other - each holds half of what the build wants - which is what makes this a merge rather
// than a fallback chain.
//
// The two halves merge differently, because the two kinds of content are shaped differently:
//
//   - A **file** entry already names one architecture, so asking each bundle in turn for the
//     entry's own request merges them for free: a bundle that does not hold that architecture
//     reports a miss and the next one answers.
//   - An **image** reference covers every architecture at once, so asking each bundle in turn
//     would let the first to hold the reference answer for all of them - and the architectures
//     it lacks would fall back to the registry even though another bundle holds them. The
//     image half therefore presents one merged view per reference. See compositeTarget.
type CompositeStore struct {
	stores []ContentStore
}

// NewCompositeStore returns a store reading from each of stores, in order. It returns the
// single store unchanged when given one, so the common case carries no merging machinery.
func NewCompositeStore(stores ...ContentStore) ContentStore {
	switch len(stores) {
	case 0:
		return nil
	case 1:
		return stores[0]
	}
	return &CompositeStore{stores: stores}
}

// File asks each bundle for the entry in turn and takes the first answer.
//
// An error stops the walk rather than moving on: a bundle reporting that an entry is ambiguous
// -- it holds the reference for several architectures and the entry names none -- has found a
// defect in the definition, and trying the next bundle would bury it.
func (c *CompositeStore) File(req contentstore.FileRequest) (string, bool, error) {
	for _, store := range c.stores {
		name, ok, err := store.File(req)
		if err != nil {
			return "", false, err
		}
		if ok {
			return name, true, nil
		}
	}
	return "", false, nil
}

// Fetch writes the file to dst, from whichever bundle holds it.
func (c *CompositeStore) Fetch(ctx context.Context, req contentstore.FileRequest, dst string) error {
	for _, store := range c.stores {
		_, ok, err := store.File(req)
		if err != nil {
			return err
		}
		if ok {
			return store.Fetch(ctx, req, dst)
		}
	}
	return fmt.Errorf("%s does not hold the file %s", c.Describe(), req.Source)
}

// HasImage reports whether any bundle holds the reference.
func (c *CompositeStore) HasImage(ref string) bool {
	for _, store := range c.stores {
		if store.HasImage(ref) {
			return true
		}
	}
	return false
}

// Target presents every bundle as one read-only target.
func (c *CompositeStore) Target() oras.ReadOnlyTarget {
	return &compositeTarget{stores: c.stores, merged: map[digest.Digest][]byte{}}
}

// Source names the bundles as the operator spelled them.
func (c *CompositeStore) Source() string {
	sources := make([]string, 0, len(c.stores))
	for _, store := range c.stores {
		sources = append(sources, store.Source())
	}
	return strings.Join(sources, ", ")
}

// Describe names the bundles in log lines.
func (c *CompositeStore) Describe() string {
	return fmt.Sprintf("%d content stores (%s)", len(c.stores), c.Source())
}

// compositeTarget is the image half of a CompositeStore: one view of a reference that several
// bundles each hold part of.
//
// When more than one bundle holds a reference, their copies are merged into an index
// synthesized here and served from memory. That is what lets the platform selection already in
// pkg/images do its job unchanged: it asks this target for the reference, gets an index
// covering every architecture any bundle holds, and fetches the child it wants - which routes
// back to whichever bundle actually has those bytes.
//
// A reference only one bundle holds is passed through untouched, so nothing is synthesized for
// the ordinary case.
type compositeTarget struct {
	stores []ContentStore

	mu sync.Mutex
	// merged holds the indexes synthesized for this target, by their own digest, so a Fetch
	// of one can be answered from memory.
	merged map[digest.Digest][]byte
}

// Resolve returns one descriptor for the reference, merging the bundles that hold it.
func (t *compositeTarget) Resolve(ctx context.Context, reference string) (ocispec.Descriptor, error) {
	type held struct {
		target oras.ReadOnlyTarget
		desc   ocispec.Descriptor
	}

	var holders []held
	for _, store := range t.stores {
		if !store.HasImage(reference) {
			continue
		}
		target := store.Target()
		desc, err := target.Resolve(ctx, reference)
		if err != nil {
			continue
		}
		holders = append(holders, held{target: target, desc: desc})
	}

	switch len(holders) {
	case 0:
		return ocispec.Descriptor{}, fmt.Errorf("%s does not hold the image %s", t.describe(), reference)
	case 1:
		return holders[0].desc, nil
	}

	// Several bundles hold it, so the merged view is an index over every platform child any
	// of them carries. A child seen twice is kept once: two bundles holding the same platform
	// are the same bytes, since a manifest is content addressed.
	var children []ocispec.Descriptor
	seen := map[digest.Digest]bool{}
	for _, holder := range holders {
		found, err := t.childrenOf(ctx, holder.target, holder.desc)
		if err != nil {
			return ocispec.Descriptor{}, err
		}
		for _, child := range found {
			if seen[child.Digest] {
				continue
			}
			seen[child.Digest] = true
			children = append(children, child)
		}
	}
	if len(children) == 0 {
		// Nothing carried a platform, so there is nothing to merge and the first answer is
		// as good as any.
		return holders[0].desc, nil
	}

	indexBytes, err := json.Marshal(ocispec.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: children,
	})
	if err != nil {
		return ocispec.Descriptor{}, err
	}
	desc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageIndex,
		Digest:    digest.FromBytes(indexBytes),
		Size:      int64(len(indexBytes)),
	}

	t.mu.Lock()
	t.merged[desc.Digest] = indexBytes
	t.mu.Unlock()
	return desc, nil
}

// childrenOf lists the platform manifests one bundle's copy of a reference offers.
func (t *compositeTarget) childrenOf(ctx context.Context, target oras.ReadOnlyTarget, desc ocispec.Descriptor) ([]ocispec.Descriptor, error) {
	if images.IsIndex(desc.MediaType) {
		rc, err := target.Fetch(ctx, desc)
		if err != nil {
			return nil, err
		}
		defer rc.Close() //nolint:errcheck // read-only, and the error says nothing a caller can act on
		body, err := io.ReadAll(rc)
		if err != nil {
			return nil, err
		}
		var index ocispec.Index
		if err := json.Unmarshal(body, &index); err != nil {
			return nil, err
		}
		var children []ocispec.Descriptor
		for _, child := range index.Manifests {
			if child.Platform == nil || child.Platform.Architecture == "" {
				continue
			}
			children = append(children, child)
		}
		return children, nil
	}

	platform, err := images.DescriptorPlatform(ctx, target, desc)
	if err != nil {
		return nil, err
	}
	if platform == nil {
		return nil, nil
	}
	child := desc
	child.Platform = platform
	return []ocispec.Descriptor{child}, nil
}

// Fetch serves a synthesized index from memory, and anything else from whichever bundle holds
// it. Blobs are content addressed, so asking each in turn is safe.
func (t *compositeTarget) Fetch(ctx context.Context, desc ocispec.Descriptor) (io.ReadCloser, error) {
	t.mu.Lock()
	body, synthesized := t.merged[desc.Digest]
	t.mu.Unlock()
	if synthesized {
		return io.NopCloser(bytes.NewReader(body)), nil
	}

	var lastErr error
	for _, store := range t.stores {
		rc, err := store.Target().Fetch(ctx, desc)
		if err != nil {
			lastErr = err
			continue
		}
		return rc, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("%s does not hold %s", t.describe(), desc.Digest)
}

// Exists reports whether any bundle holds the content.
func (t *compositeTarget) Exists(ctx context.Context, desc ocispec.Descriptor) (bool, error) {
	t.mu.Lock()
	_, synthesized := t.merged[desc.Digest]
	t.mu.Unlock()
	if synthesized {
		return true, nil
	}
	for _, store := range t.stores {
		ok, err := store.Target().Exists(ctx, desc)
		if err != nil {
			continue
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func (t *compositeTarget) describe() string {
	sources := make([]string, 0, len(t.stores))
	for _, store := range t.stores {
		sources = append(sources, store.Source())
	}
	return strings.Join(sources, ", ")
}
