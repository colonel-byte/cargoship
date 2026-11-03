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

package distro

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/coci"
	"github.com/colonel-byte/cargoship/pkg/coci/layers"
	"github.com/colonel-byte/cargoship/pkg/helpers"
	"github.com/colonel-byte/cargoship/pkg/packager/layout"
	"github.com/colonel-byte/cargoship/pkg/signing"
	"github.com/colonel-byte/cargoship/pkg/utils"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/pkg/archive"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/oci"
	orasoci "oras.land/oras-go/v2/content/oci"
)

// ImportOptions declares optional parameters for an Import operation.
type ImportOptions struct {
	Architecture         string
	Output               string
	VerifyBlobOptions    *signing.VerifyBlobOptions
	VerificationStrategy layout.VerificationStrategy
}

// Import converts an OCI Image Layout archive or directory, such as one written by
// `skopeo copy ... oci-archive:`, into a native Cargoship package.
func Import(ctx context.Context, source string, opts ImportOptions) (string, error) {
	if source == "" {
		return "", errors.New("must provide an archive source path")
	}
	if opts.Output == "" {
		return "", errors.New("no output directory specified")
	}

	disLayout, err := ImportLayout(ctx, source, opts)
	if err != nil {
		return "", err
	}
	defer disLayout.Cleanup() //nolint:errcheck

	return disLayout.Archive(ctx, opts.Output, 0)
}

// ImportLayout reads an OCI Image Layout archive or directory and unpacks it into a DistroLayout.
func ImportLayout(ctx context.Context, source string, opts ImportOptions) (*layout.DistroLayout, error) {
	tempWorkspace, err := utils.MakeTempDir(config.CommonOptions.TempDirectory)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempWorkspace) //nolint:errcheck

	ociDir := source
	info, err := os.Stat(source)
	if err != nil {
		return nil, fmt.Errorf("unable to read source %q: %w", source, err)
	}

	if !info.IsDir() {
		ociDir = filepath.Join(tempWorkspace, "extracted")
		if err := helpers.CreateDirectory(ociDir, helpers.ReadWriteExecuteUser); err != nil {
			return nil, err
		}
		if err := archive.Decompress(ctx, source, ociDir, archive.DecompressOpts{}); err != nil {
			return nil, fmt.Errorf("failed to decompress archive %s: %w", source, err)
		}
	}

	if _, err := os.Stat(filepath.Join(ociDir, ocispec.ImageIndexFile)); err != nil {
		return nil, fmt.Errorf("%s is not an OCI Image Layout: no %s found", source, ocispec.ImageIndexFile)
	}

	return importOCIArchive(ctx, ociDir, opts)
}

func importOCIArchive(ctx context.Context, ociDir string, opts ImportOptions) (*layout.DistroLayout, error) {
	store, err := orasoci.New(ociDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open OCI store at %s: %w", ociDir, err)
	}

	indexBytes, err := os.ReadFile(filepath.Join(ociDir, ocispec.ImageIndexFile))
	if err != nil {
		return nil, fmt.Errorf("reading OCI index: %w", err)
	}
	var index ocispec.Index
	if err := json.Unmarshal(indexBytes, &index); err != nil {
		return nil, fmt.Errorf("unmarshaling OCI index: %w", err)
	}

	manifestDesc, err := selectManifest(ctx, store, index, config.GetArch(opts.Architecture))
	if err != nil {
		return nil, err
	}

	manifestBytes, err := fetchBytes(ctx, store, manifestDesc)
	if err != nil {
		return nil, fmt.Errorf("reading manifest %s: %w", manifestDesc.Digest, err)
	}
	var ociManifest ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &ociManifest); err != nil {
		return nil, fmt.Errorf("unmarshaling manifest %s: %w", manifestDesc.Digest, err)
	}

	root := &oci.Manifest{Manifest: ociManifest}
	layersToExtract, err := coci.AssembleLayers(ctx, root, store, layers.GetAllLayerTypes()...)
	if err != nil {
		return nil, fmt.Errorf("assembling layers: %w", err)
	}

	pkgDir, err := utils.MakeTempDir(config.CommonOptions.TempDirectory)
	if err != nil {
		return nil, err
	}

	disLayout, err := extractPackage(ctx, store, layersToExtract, pkgDir, opts)
	if err != nil {
		os.RemoveAll(pkgDir) //nolint:errcheck
		return nil, err
	}

	disLayout.SetRegistryDigest(manifestDesc.Digest.String())
	return disLayout, nil
}

// extractPackage writes every titled layer into pkgDir and loads the result as a package.
// A layer with no title is not a file, so it is skipped.
func extractPackage(ctx context.Context, store *orasoci.Store, layersToExtract []ocispec.Descriptor, pkgDir string, opts ImportOptions) (*layout.DistroLayout, error) {
	for _, layer := range layersToExtract {
		title := layer.Annotations[ocispec.AnnotationTitle]
		if title == "" {
			continue
		}
		// The title is the one string from the archive that becomes a path on this machine.
		// AssembleLayers only selects titles cargoship computed, so nothing reaching here
		// escapes today; the check is what keeps that true of a future caller.
		if !layout.IsContainedPath(title) {
			return nil, fmt.Errorf("layer %s declares the title %q, which does not stay inside the package", layer.Digest, title)
		}
		if err := writeLayer(ctx, store, layer, filepath.Join(pkgDir, title)); err != nil {
			return nil, err
		}
	}

	disLayout, err := layout.LoadFromDir(ctx, pkgDir, layout.DistroLayoutOptions{
		VerifyBlobOptions:    opts.VerifyBlobOptions,
		VerificationStrategy: opts.VerificationStrategy,
	})
	if err != nil {
		return nil, fmt.Errorf("loading extracted package: %w", err)
	}
	return disLayout, nil
}

func writeLayer(ctx context.Context, store *orasoci.Store, layer ocispec.Descriptor, destPath string) error {
	if err := helpers.CreateParentDirectory(destPath); err != nil {
		return fmt.Errorf("creating parent dir for %s: %w", destPath, err)
	}

	rc, err := store.Fetch(ctx, layer)
	if err != nil {
		return fmt.Errorf("fetching layer %s (%s): %w", layer.Digest, destPath, err)
	}
	defer rc.Close() //nolint:errcheck

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, helpers.ReadAllWriteUser)
	if err != nil {
		return fmt.Errorf("creating file %s: %w", destPath, err)
	}
	if _, err := io.Copy(f, rc); err != nil {
		f.Close() //nolint:errcheck
		return fmt.Errorf("writing layer to %s: %w", destPath, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing file %s: %w", destPath, err)
	}
	return nil
}

func fetchBytes(ctx context.Context, store *orasoci.Store, desc ocispec.Descriptor) ([]byte, error) {
	rc, err := store.Fetch(ctx, desc)
	if err != nil {
		return nil, err
	}
	defer rc.Close() //nolint:errcheck
	return io.ReadAll(rc)
}

// selectManifest picks the package manifest to import out of an index that may carry
// several, as an archive built by appending images does.
func selectManifest(ctx context.Context, store *orasoci.Store, index ocispec.Index, targetArch string) (ocispec.Descriptor, error) {
	if len(index.Manifests) == 0 {
		return ocispec.Descriptor{}, errors.New("OCI index contains no manifests")
	}

	// A nested index is how a multi-arch package records its per-arch manifests, so descend
	// before matching on architecture: the nested descriptor is not itself a manifest.
	for _, m := range index.Manifests {
		if m.MediaType != ocispec.MediaTypeImageIndex {
			continue
		}
		data, err := fetchBytes(ctx, store, m)
		if err != nil {
			continue
		}
		var subIndex ocispec.Index
		if err := json.Unmarshal(data, &subIndex); err != nil {
			continue
		}
		if chosen, err := selectManifest(ctx, store, subIndex, targetArch); err == nil {
			return chosen, nil
		}
	}

	if targetArch != "" {
		for _, m := range index.Manifests {
			if m.Platform != nil && m.Platform.Architecture == targetArch {
				return m, nil
			}
		}
	}

	if len(index.Manifests) > 1 {
		logger.From(ctx).Debug("multiple manifests found in OCI index; using the first", "count", len(index.Manifests))
	}
	return index.Manifests[0], nil
}
