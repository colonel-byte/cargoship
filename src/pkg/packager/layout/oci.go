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

package layout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/internal/cfg"
	"github.com/colonel-byte/cargoship/src/pkg/helpers"
	godigest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/api/v1alpha1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/errdef"
)

const (
	// CargoshipLayerMediaTypeBlob is the media type for all Cargoship package layer blobs.
	CargoshipLayerMediaTypeBlob = "application/vnd.cargoship.layer.v1.blob"
	// CargoshipConfigMediaType is the media type for the Cargoship package manifest config.
	CargoshipConfigMediaType = "application/vnd.cargoship.config.v1+json"
	// OCITimestampFormat is the format used for the OCI timestamp annotation
	OCITimestampFormat = time.RFC3339
)

// manifestCache holds the computed OCI manifest for the package layout.
// Populated by computeManifest; nil until then.
type manifestCache struct {
	desc         ocispec.Descriptor
	manifestJSON []byte
	configBytes  []byte
	configDigest godigest.Digest
	blobs        map[godigest.Digest]string // layer digest → file path
	totalSize    int64                      // layers + config + manifest
}

// AnnotationsFromMetadata extracts OCI manifest annotations from Zarf package metadata.
func AnnotationsFromMetadata(metadata distro.ZarfDistroMetadata) map[string]string {
	annotations := map[string]string{
		ocispec.AnnotationTitle:       metadata.Name,
		ocispec.AnnotationDescription: metadata.Description,
	}
	if url := metadata.URL; url != "" {
		annotations[ocispec.AnnotationURL] = url
	}
	if authors := metadata.Authors; authors != "" {
		annotations[ocispec.AnnotationAuthors] = authors
	}
	if documentation := metadata.Documentation; documentation != "" {
		annotations[ocispec.AnnotationDocumentation] = documentation
	}
	if source := metadata.Source; source != "" {
		annotations[ocispec.AnnotationSource] = source
	}
	if vendor := metadata.Vendor; vendor != "" {
		annotations[ocispec.AnnotationVendor] = vendor
	}
	// annotations explicitly defined in metadata.Annotations take precedence over legacy fields.
	maps.Copy(annotations, metadata.Annotations)
	return annotations
}

// Exists implements oras.ReadOnlyTarget.
func (d *DistroLayout) Exists(_ context.Context, target ocispec.Descriptor) (bool, error) {
	if d.cache == nil {
		return false, nil
	}
	if target.Digest == d.cache.desc.Digest || target.Digest == d.cache.configDigest {
		return true, nil
	}
	_, ok := d.cache.blobs[target.Digest]
	return ok, nil
}

// Fetch implements oras.ReadOnlyTarget. It serves the manifest, config, or a
// layer blob identified by the descriptor's digest.
func (d *DistroLayout) Fetch(_ context.Context, target ocispec.Descriptor) (io.ReadCloser, error) {
	if d.cache == nil {
		return nil, errdef.ErrNotFound
	}
	switch target.Digest {
	case d.cache.desc.Digest:
		return io.NopCloser(bytes.NewReader(d.cache.manifestJSON)), nil
	case d.cache.configDigest:
		return io.NopCloser(bytes.NewReader(d.cache.configBytes)), nil
	}
	if filePath, ok := d.cache.blobs[target.Digest]; ok {
		return os.Open(filePath)
	}
	return nil, errdef.ErrNotFound
}

// Resolve implements oras.ReadOnlyTarget. It accepts the manifest digest or the
// package name as a reference.
func (d *DistroLayout) Resolve(_ context.Context, reference string) (ocispec.Descriptor, error) {
	if d.cache == nil {
		return ocispec.Descriptor{}, errdef.ErrNotFound
	}
	if reference == d.digest || reference == d.Distro.Metadata.Name {
		return d.cache.desc, nil
	}
	return ocispec.Descriptor{}, errdef.ErrNotFound
}

// computeManifest builds the OCI manifest for this layout, caches the result,
// and sets d.digest.
//
// SHA256s for most files are read from checksums.txt (already computed at build
// time), so only the small files excluded from that list (zarf.yaml, checksums.txt
// itself, and post-signing provenance files) are read from disk.
func (d *DistroLayout) computeManifest(ctx context.Context) error {
	// Parse checksums.txt into relpath → sha256hex.
	checksumsPath := filepath.Join(d.dirPath, config.Checksums)
	checksumsBytes, err := os.ReadFile(checksumsPath)
	if err != nil {
		return fmt.Errorf("reading checksums file: %w", err)
	}
	checksumMap := map[string]string{}
	for _, line := range strings.Split(string(checksumsBytes), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid checksum line: %q", line)
		}
		checksumMap[parts[1]] = parts[0] // relpath → sha256hex
	}

	files, err := d.Files()
	if err != nil {
		return err
	}

	var (
		descs          []ocispec.Descriptor
		totalLayerSize int64
		blobs          = map[godigest.Digest]string{}
	)
	for filePath, name := range files {
		rel, err := filepath.Rel(d.dirPath, filePath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		var fileDigest godigest.Digest
		var fileSize int64

		switch {
		case checksumMap[rel] != "":
			// Pre-computed hash; only stat for size (no file read).
			fileDigest, err = godigest.Parse("sha256:" + checksumMap[rel])
			if err != nil {
				return fmt.Errorf("invalid checksum for %q: %w", rel, err)
			}
			info, err := os.Stat(filePath)
			if err != nil {
				return err
			}
			fileSize = info.Size()
		case rel == config.Checksums:
			// checksums.txt is excluded from its own content but is a layer; we
			// already have its bytes from the read above.
			fileDigest = godigest.FromBytes(checksumsBytes)
			fileSize = int64(len(checksumsBytes))
		default:
			// zarf.yaml and post-signing provenance files (signature, bundle) are
			// small — read from disk.
			hex, err := helpers.GetSHA256OfFile(filePath)
			if err != nil {
				return err
			}
			fileDigest, err = godigest.Parse("sha256:" + hex)
			if err != nil {
				return fmt.Errorf("computing sha256 for %q: %w", rel, err)
			}
			info, err := os.Stat(filePath)
			if err != nil {
				return err
			}
			fileSize = info.Size()
		}

		descs = append(descs, ocispec.Descriptor{
			MediaType: CargoshipLayerMediaTypeBlob,
			Digest:    fileDigest,
			Size:      fileSize,
			Annotations: map[string]string{
				ocispec.AnnotationTitle: name,
			},
		})
		blobs[fileDigest] = filePath
		totalLayerSize += fileSize
	}

	// Sort by digest for deterministic ordering.
	sort.Slice(descs, func(i, j int) bool {
		return descs[i].Digest.String() < descs[j].Digest.String()
	})

	// Read the zarf.yaml from disk rather than using d.Pkg, which may have been
	// component-filtered or otherwise mutated after load.
	zarfYAMLBytes, err := os.ReadFile(filepath.Join(d.dirPath, config.DistroYAML))
	if err != nil {
		return fmt.Errorf("reading %s for manifest: %w", config.DistroYAML, err)
	}
	zarfPkg, err := cfg.ParseMultiDoc(ctx, zarfYAMLBytes)
	if err != nil {
		return fmt.Errorf("parsing %s for manifest: %w", config.DistroYAML, err)
	}
	configBytes, err := json.Marshal(zarfPkg)
	if err != nil {
		return err
	}
	configDesc := content.NewDescriptorFromBytes(CargoshipConfigMediaType, configBytes)

	annotations := AnnotationsFromMetadata(zarfPkg.Metadata)

	// Back-compatible timestamp parsing → OCI format. Fall back to zero time (epoch) if the timestamp is absent.
	t, parseErr := time.Parse(v1alpha1.BuildTimestampFormat, zarfPkg.Build.Timestamp)
	if parseErr != nil {
		t = time.Time{}
	}
	annotations[ocispec.AnnotationCreated] = t.UTC().Format(OCITimestampFormat)

	memStore := memory.New()
	root, err := oras.PackManifest(ctx, memStore, oras.PackManifestVersion1_1, "", oras.PackManifestOptions{
		Layers:              descs,
		ConfigDescriptor:    &configDesc,
		ManifestAnnotations: annotations,
	})
	if err != nil {
		return fmt.Errorf("unable to pack manifest: %w", err)
	}

	manifestReader, err := memStore.Fetch(ctx, root)
	if err != nil {
		return fmt.Errorf("fetching packed manifest: %w", err)
	}
	manifestJSON, readErr := io.ReadAll(manifestReader)
	if err := errors.Join(readErr, manifestReader.Close()); err != nil {
		return fmt.Errorf("reading packed manifest: %w", err)
	}

	d.cache = &manifestCache{
		desc:         root,
		manifestJSON: manifestJSON,
		configBytes:  configBytes,
		configDigest: configDesc.Digest,
		blobs:        blobs,
		totalSize:    totalLayerSize + int64(len(configBytes)) + root.Size,
	}
	d.digest = root.Digest.String()
	return nil
}

// SetRegistryDigest records the manifest digest as resolved from a registry.
// It replaces the locally-computed digest and clears the manifest cache, since
// the registry manifest may differ (e.g. partial OCI pulls). After this call
// the layout is no longer usable as an oras.ReadOnlyTarget for pushing.
func (d *DistroLayout) SetRegistryDigest(digest string) {
	d.digest = digest
	d.cache = nil
}

// IsPushable reports whether this layout has a computed manifest cache and can
// be used as a push source. A layout with only a registry digest (e.g. from a
// partial OCI pull via SetRegistryDigest) returns false because the cache is nil.
func (d *DistroLayout) IsPushable() bool {
	return d.cache != nil
}

// TotalSize returns the total bytes that would be pushed for this package (all
// layers + config + manifest). Returns 0 if the manifest has not been computed.
func (d *DistroLayout) TotalSize() int64 {
	if d.cache == nil {
		return 0
	}
	return d.cache.totalSize
}
