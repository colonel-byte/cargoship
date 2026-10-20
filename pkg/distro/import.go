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
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/coci"
	"github.com/colonel-byte/cargoship/pkg/coci/layers"
	"github.com/colonel-byte/cargoship/pkg/helpers"
	"github.com/colonel-byte/cargoship/pkg/packager/layout"
	"github.com/colonel-byte/cargoship/pkg/signing"
	"github.com/colonel-byte/cargoship/pkg/utils"
	"github.com/defenseunicorns/pkg/oci"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/pkg/archive"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	orasoci "oras.land/oras-go/v2/content/oci"
)

// ImportFormat describes the archive format being imported.
type ImportFormat string

const (
	// ImportFormatAuto automatically detects between oci-archive and docker-archive.
	ImportFormatAuto ImportFormat = "auto"
	// ImportFormatOCIArchive indicates a standard OCI Image Layout archive or directory.
	ImportFormatOCIArchive ImportFormat = "oci-archive"
	// ImportFormatDockerArchive indicates a Docker image archive (docker save format).
	ImportFormatDockerArchive ImportFormat = "docker-archive"
)

// ImportOptions declares optional parameters for an Import operation.
type ImportOptions struct {
	Format               ImportFormat
	Architecture         string
	Output               string
	VerifyBlobOptions    *signing.VerifyBlobOptions
	VerificationStrategy layout.VerificationStrategy
}

// Import converts an image archive (Skopeo oci-archive or docker-archive) into a native Cargoship package.
func Import(ctx context.Context, source string, opts ImportOptions) (string, error) {
	if source == "" {
		return "", errors.New("must provide an archive source path")
	}

	outputDir := opts.Output
	if outputDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		outputDir = wd
	}

	disLayout, err := ImportLayout(ctx, source, opts)
	if err != nil {
		return "", err
	}
	defer disLayout.Cleanup() //nolint:errcheck

	return disLayout.Archive(ctx, outputDir, 0)
}

// ImportLayout reads an OCI or Docker archive/directory and unpacks it into a DistroLayout.
func ImportLayout(ctx context.Context, source string, opts ImportOptions) (*layout.DistroLayout, error) {
	tempWorkspace, err := utils.MakeTempDir(config.CommonOptions.TempDirectory)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempWorkspace) //nolint:errcheck

	extractedDir := source
	info, err := os.Stat(source)
	if err != nil {
		return nil, fmt.Errorf("unable to read source %q: %w", source, err)
	}

	if !info.IsDir() {
		extractedDir = filepath.Join(tempWorkspace, "extracted")
		if err := helpers.CreateDirectory(extractedDir, helpers.ReadWriteExecuteUser); err != nil {
			return nil, err
		}
		if err := archive.Decompress(ctx, source, extractedDir, archive.DecompressOpts{}); err != nil {
			return nil, fmt.Errorf("failed to decompress archive %s: %w", source, err)
		}
	}

	format := opts.Format
	if format == "" || format == ImportFormatAuto {
		if _, err := os.Stat(filepath.Join(extractedDir, ocispec.ImageLayoutFile)); err == nil {
			format = ImportFormatOCIArchive
		} else if _, err := os.Stat(filepath.Join(extractedDir, "manifest.json")); err == nil {
			format = ImportFormatDockerArchive
		} else if _, err := os.Stat(filepath.Join(extractedDir, ocispec.ImageIndexFile)); err == nil {
			format = ImportFormatOCIArchive
		} else {
			return nil, errors.New("unable to determine archive format: neither oci-layout nor manifest.json found")
		}
	}

	switch format {
	case ImportFormatDockerArchive:
		return importDockerArchive(ctx, extractedDir, opts)
	case ImportFormatOCIArchive:
		return importOCIArchive(ctx, extractedDir, opts)
	default:
		return nil, fmt.Errorf("unsupported import format: %s", format)
	}
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

	targetArch := config.GetArch(opts.Architecture)
	manifestDesc, err := selectManifest(ctx, store, index, targetArch)
	if err != nil {
		return nil, err
	}

	manifestRC, err := store.Fetch(ctx, manifestDesc)
	if err != nil {
		return nil, fmt.Errorf("fetching manifest %s: %w", manifestDesc.Digest, err)
	}
	defer manifestRC.Close() //nolint:errcheck

	manifestBytes, err := io.ReadAll(manifestRC)
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

	for _, layer := range layersToExtract {
		title := layer.Annotations[ocispec.AnnotationTitle]
		if title == "" {
			continue
		}
		destPath := filepath.Join(pkgDir, title)
		if err := helpers.CreateParentDirectory(destPath); err != nil {
			os.RemoveAll(pkgDir) //nolint:errcheck
			return nil, fmt.Errorf("creating parent dir for %s: %w", title, err)
		}

		rc, err := store.Fetch(ctx, layer)
		if err != nil {
			os.RemoveAll(pkgDir) //nolint:errcheck
			return nil, fmt.Errorf("fetching layer %s (%s): %w", layer.Digest, title, err)
		}

		f, err := os.OpenFile(destPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, helpers.ReadAllWriteUser)
		if err != nil {
			rc.Close()           //nolint:errcheck
			os.RemoveAll(pkgDir) //nolint:errcheck
			return nil, fmt.Errorf("creating file %s: %w", destPath, err)
		}

		_, copyErr := io.Copy(f, rc)
		closeErr := f.Close()
		rc.Close() //nolint:errcheck

		if copyErr != nil {
			os.RemoveAll(pkgDir) //nolint:errcheck
			return nil, fmt.Errorf("writing layer to %s: %w", destPath, copyErr)
		}
		if closeErr != nil {
			os.RemoveAll(pkgDir) //nolint:errcheck
			return nil, fmt.Errorf("closing file %s: %w", destPath, closeErr)
		}
	}

	disLayout, err := layout.LoadFromDir(ctx, pkgDir, layout.DistroLayoutOptions{
		VerifyBlobOptions:    opts.VerifyBlobOptions,
		VerificationStrategy: opts.VerificationStrategy,
	})
	if err != nil {
		os.RemoveAll(pkgDir) //nolint:errcheck
		return nil, fmt.Errorf("loading extracted package: %w", err)
	}

	disLayout.SetRegistryDigest(manifestDesc.Digest.String())
	return disLayout, nil
}

type dockerManifestItem struct {
	Config   string   `json:"Config"`
	RepoTags []string `json:"RepoTags"`
	Layers   []string `json:"Layers"`
}

func importDockerArchive(ctx context.Context, dir string, opts ImportOptions) (*layout.DistroLayout, error) {
	manifestBytes, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("reading docker manifest.json: %w", err)
	}
	var manifests []dockerManifestItem
	if err := json.Unmarshal(manifestBytes, &manifests); err != nil {
		return nil, fmt.Errorf("unmarshaling docker manifest.json: %w", err)
	}
	if len(manifests) == 0 {
		return nil, errors.New("docker archive manifest.json contains no image entries")
	}

	// In a docker save of an OCI artifact or image, find the entry and convert layers to an OCI store.
	ociDir, err := utils.MakeTempDir(config.CommonOptions.TempDirectory)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(ociDir) //nolint:errcheck

	if err := os.WriteFile(filepath.Join(ociDir, ocispec.ImageLayoutFile), []byte(`{"imageLayoutVersion":"1.0.0"}`), 0644); err != nil {
		return nil, err
	}
	blobsDir := filepath.Join(ociDir, "blobs", "sha256")
	if err := helpers.CreateDirectory(blobsDir, helpers.ReadWriteExecuteUser); err != nil {
		return nil, err
	}

	// Build an OCI image manifest from the docker archive layers
	item := manifests[0]
	var layerDescs []ocispec.Descriptor

	for _, layerRel := range item.Layers {
		layerPath := filepath.Join(dir, layerRel)
		contentBytes, title, err := readDockerLayerBlob(layerPath)
		if err != nil {
			return nil, fmt.Errorf("reading docker layer %s: %w", layerRel, err)
		}
		d := digest.FromBytes(contentBytes)
		blobDest := filepath.Join(blobsDir, d.Encoded())
		if err := os.WriteFile(blobDest, contentBytes, 0644); err != nil {
			return nil, err
		}

		desc := ocispec.Descriptor{
			MediaType: "application/vnd.cargoship.layer.v1.blob",
			Digest:    d,
			Size:      int64(len(contentBytes)),
		}
		if title != "" {
			desc.Annotations = map[string]string{
				ocispec.AnnotationTitle: title,
			}
		}
		layerDescs = append(layerDescs, desc)
	}

	// Read config
	var configDesc ocispec.Descriptor
	if item.Config != "" {
		cfgBytes, err := os.ReadFile(filepath.Join(dir, item.Config))
		if err == nil {
			cd := digest.FromBytes(cfgBytes)
			if err := os.WriteFile(filepath.Join(blobsDir, cd.Encoded()), cfgBytes, 0644); err == nil {
				configDesc = ocispec.Descriptor{
					MediaType: "application/vnd.cargoship.config.v1+json",
					Digest:    cd,
					Size:      int64(len(cfgBytes)),
				}
			}
		}
	}
	if configDesc.Digest == "" {
		emptyCfg := []byte("{}")
		cd := digest.FromBytes(emptyCfg)
		if err := os.WriteFile(filepath.Join(blobsDir, cd.Encoded()), emptyCfg, 0644); err != nil {
			return nil, err
		}
		configDesc = ocispec.Descriptor{
			MediaType: "application/vnd.cargoship.config.v1+json",
			Digest:    cd,
			Size:      int64(len(emptyCfg)),
		}
	}

	// Inspect the layers to match annotations if any layer is a distro.yaml / checksums.txt
	inspectAndAnnotateLayers(layerDescs, blobsDir)

	manifest := ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    layerDescs,
	}
	mBytes, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	mDigest := digest.FromBytes(mBytes)
	if err := os.WriteFile(filepath.Join(blobsDir, mDigest.Encoded()), mBytes, 0644); err != nil {
		return nil, err
	}

	targetArch := config.GetArch(opts.Architecture)
	index := ocispec.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: []ocispec.Descriptor{
			{
				MediaType: ocispec.MediaTypeImageManifest,
				Digest:    mDigest,
				Size:      int64(len(mBytes)),
				Platform: &ocispec.Platform{
					Architecture: targetArch,
					OS:           "linux",
				},
			},
		},
	}
	idxBytes, err := json.Marshal(index)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(ociDir, ocispec.ImageIndexFile), idxBytes, 0644); err != nil {
		return nil, err
	}

	return importOCIArchive(ctx, ociDir, opts)
}

func readDockerLayerBlob(path string) ([]byte, string, error) {
	// If path is a tar file containing a single file, or is raw:
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close() //nolint:errcheck

	tr := tar.NewReader(f)
	hdr, err := tr.Next()
	if err == nil && hdr != nil {
		name := hdr.Name
		data, readErr := io.ReadAll(tr)
		if readErr == nil {
			// Check if there are other files in tar
			_, secondErr := tr.Next()
			if secondErr == io.EOF {
				return data, name, nil
			}
		}
	}

	// Otherwise read entire file as blob
	b, err := os.ReadFile(path)
	return b, "", err
}

func inspectAndAnnotateLayers(layers []ocispec.Descriptor, blobsDir string) {
	for i := range layers {
		blobPath := filepath.Join(blobsDir, layers[i].Digest.Encoded())
		data, err := os.ReadFile(blobPath)
		if err != nil {
			continue
		}
		if layers[i].Annotations == nil {
			layers[i].Annotations = make(map[string]string)
		}
		contentStr := string(data)
		if strings.Contains(contentStr, "apiVersion: colonel-byte.dev/v1alpha1") && strings.Contains(contentStr, "kind: Distro") {
			layers[i].Annotations[ocispec.AnnotationTitle] = config.DistroYAML
		} else if strings.Contains(contentStr, " distro.yaml") || (len(data) > 0 && strings.Contains(contentStr, "files/")) {
			layers[i].Annotations[ocispec.AnnotationTitle] = config.Checksums
		}
	}
}

func selectManifest(ctx context.Context, store *orasoci.Store, index ocispec.Index, targetArch string) (ocispec.Descriptor, error) {
	if len(index.Manifests) == 0 {
		return ocispec.Descriptor{}, errors.New("OCI index contains no manifests")
	}

	for _, m := range index.Manifests {
		if m.MediaType == ocispec.MediaTypeImageIndex {
			rc, err := store.Fetch(ctx, m)
			if err != nil {
				continue
			}
			data, err := io.ReadAll(rc)
			rc.Close() //nolint:errcheck
			if err != nil {
				continue
			}
			var subIndex ocispec.Index
			if err := json.Unmarshal(data, &subIndex); err == nil {
				if chosen, err := selectManifest(ctx, store, subIndex, targetArch); err == nil {
					return chosen, nil
				}
			}
		}
	}

	if targetArch != "" {
		for _, m := range index.Manifests {
			if m.Platform != nil && m.Platform.Architecture == targetArch {
				return m, nil
			}
		}
	}

	distinct := make(map[string]ocispec.Descriptor)
	for _, m := range index.Manifests {
		if m.MediaType != ocispec.MediaTypeImageIndex {
			distinct[m.Digest.String()] = m
		}
	}

	if len(distinct) == 1 {
		for _, m := range distinct {
			return m, nil
		}
	}

	if len(index.Manifests) == 1 {
		return index.Manifests[0], nil
	}

	logger.From(ctx).Debug("multiple manifests found in OCI index; using first manifest", "count", len(index.Manifests))
	return index.Manifests[0], nil
}
