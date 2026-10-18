// Copyright 2026 colonel-byte
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package release publishes release artifacts that goreleaser does not build.
//
// Only the OpenTofu provider lives here. GoReleaser builds every other binary the repository
// releases and could build this one too, but what has to be published is not a release asset: it
// is an OCI image index assembled to the shape an OpenTofu provider mirror serves, and goreleaser
// has nothing to say about composing one. Building the zips here as well keeps the whole artifact
// in one place, and lets them carry a fixed entry timestamp so the same commit produces the same
// bytes.
package release

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/magefile/mage/sh"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
)

const (
	// providerPackage is the main package the provider is built from. It is a package of this
	// module rather than a module of its own, which is why this is an ordinary package path and
	// the build below needs no -C. See docs/agent/choice-tofu-provider-layout.md.
	providerPackage = "./cmd/terraform-provider-cargoship"
	// providerImportPath is the provider package whose Version the build stamps.
	providerImportPath = "github.com/colonel-byte/cargoship/internal/tofuprovider"
	// providerBinaryBase is the binary name the zip has to carry, without the version. OpenTofu
	// looks for terraform-provider-<name>_v<version> inside the package, so this is not ours to
	// choose.
	providerBinaryBase = "terraform-provider-cargoship"
	// providerRepository is where the artifact is published. It is the repository the oci_mirror
	// example in docs/agent/choice-tofu-provider-layout.md resolves to.
	providerRepository = "ghcr.io/colonel-byte/tofu-providers/colonel-byte/cargoship"
	// providerOutputDir holds the zips and the OCI layout. It is under build/, which is ignored.
	providerOutputDir = "build/tofu-provider"
	// providerMirrorPath is where a filesystem_mirror expects a provider: the registry hostname
	// the configuration names, then the namespace, then the type. The hostname is part of it even
	// for a provider no registry serves, because that is the source address a configuration
	// resolves -- see docs/agent/choice-tofu-provider-layout.md.
	providerMirrorPath = "registry.opentofu.org/colonel-byte/cargoship"

	// artifactTypeIndex and artifactTypeTarget are what OpenTofu looks for when it reads a
	// provider out of an OCI registry: the index declares the first and each per-platform
	// manifest the second. A client that finds neither does not recognise the artifact as a
	// provider at all.
	artifactTypeIndex  = "application/vnd.opentofu.provider"
	artifactTypeTarget = "application/vnd.opentofu.provider-target"
	// layerMediaType is the media type of the one layer each platform manifest carries.
	layerMediaType = "archive/zip"
)

// providerPlatforms are the platforms the provider is published for. A client whose platform is
// missing from the index fails to install rather than falling back to another one, so dropping a
// platform from this list is a user-visible decision.
var providerPlatforms = [][2]string{
	// keep-sorted start
	{"darwin", "amd64"},
	{"darwin", "arm64"},
	{"linux", "amd64"},
	{"linux", "arm64"},
	{"windows", "amd64"},
	// keep-sorted end
}

// zipEpoch is the modification time every zip entry carries. A fixed time is what makes two builds
// of the same commit produce the same bytes, which is worth more here than the real timestamp: the
// digest is what a mirror serves and what an operator compares.
var zipEpoch = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

// Options are the inputs to TofuProvider.
type Options struct {
	// Version is the release version without the leading v: 0.1.0, or 0.1.0+build.1.
	Version string
	// Repository is the OCI repository to publish to. Empty means providerRepository.
	Repository string
	// Push copies the assembled index to Repository. False stops after the layout is built,
	// which is how the artifact shape is checked without a registry.
	Push bool
}

// TofuProvider builds the OpenTofu provider for every published platform and assembles it into an
// OCI image index an OpenTofu provider mirror can serve, optionally pushing it.
//
// The shape is defined by the provider mirror protocol: an index whose artifactType is
// application/vnd.opentofu.provider, one manifest per platform whose artifactType is
// application/vnd.opentofu.provider-target and which carries a platform property, and a single
// archive/zip layer holding the provider package. It needs no GPG signature and no registry
// listing, which is why this is the distribution path rather than a registry submission. See
// docs/agent/choice-tofu-provider-layout.md and
// https://opentofu.org/docs/cli/oci_registries/provider-mirror/.
func TofuProvider(opts Options) error {
	if err := validateVersion(opts.Version); err != nil {
		return err
	}

	ctx := context.Background()
	repository := opts.Repository
	if repository == "" {
		repository = providerRepository
	}

	// The OCI tag grammar has no "+", and OpenTofu reads "_" in its place.
	tag := strings.ReplaceAll(opts.Version, "+", "_")

	if err := os.RemoveAll(providerOutputDir); err != nil {
		return err
	}
	layout := filepath.Join(providerOutputDir, "layout")
	if err := os.MkdirAll(layout, 0o755); err != nil {
		return err
	}

	store, err := oci.New(layout)
	if err != nil {
		return fmt.Errorf("initializing OCI layout store at %s: %w", layout, err)
	}

	manifestDescs := make([]ocispec.Descriptor, 0, len(providerPlatforms))

	for _, platform := range providerPlatforms {
		oper, arch := platform[0], platform[1]

		archive, err := buildPlatform(opts.Version, oper, arch)
		if err != nil {
			return err
		}

		manifestTag := fmt.Sprintf("%s_%s", oper, arch)
		manifestDesc, err := pushPlatform(ctx, store, manifestTag, oper, arch, archive)
		if err != nil {
			return err
		}
		manifestDescs = append(manifestDescs, manifestDesc)
	}

	fmt.Printf("assembling the index as %s\n", tag)
	if err := assembleIndex(ctx, store, tag, manifestDescs); err != nil {
		return fmt.Errorf("assembling the provider index: %w", err)
	}

	if !opts.Push {
		fmt.Printf("not pushing: the layout is at %s\n", layout)
		return nil
	}

	target := repository + ":" + tag
	fmt.Printf("pushing %s\n", target)
	if err := pushIndexToRemote(ctx, store, repository, tag); err != nil {
		return fmt.Errorf("pushing %s: %w", target, err)
	}
	return nil
}

// TofuProviderHost builds the provider for this machine only, into the layout OpenTofu's
// filesystem_mirror reads: build/tofu-provider/mirror/<hostname>/<namespace>/<type>/<version>/<os>_<arch>/.
//
// It exists because the publish path is the wrong loop for development. A mirror directory is what
// `tofu init` reads with no registry, no OCI push and no zip, so a change can be tried in the time
// a build takes. See docs/dev/tofu-provider.md.
func TofuProviderHost(version string) (string, error) {
	if err := validateVersion(version); err != nil {
		return "", err
	}

	mirror := filepath.Join(providerOutputDir, "mirror", providerMirrorPath, version,
		fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH))
	if err := os.MkdirAll(mirror, 0o755); err != nil {
		return "", err
	}

	binary := fmt.Sprintf("%s_v%s", providerBinaryBase, version)
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	path := filepath.Join(mirror, binary)

	fmt.Printf("building %s %s for %s/%s\n", providerBinaryBase, version, runtime.GOOS, runtime.GOARCH)
	ldflags := fmt.Sprintf("-X main.version=%s -X %s.Version=%s", version, providerImportPath, version)
	if err := sh.RunV("go", "build", "-ldflags", ldflags, "-o", path, providerPackage); err != nil {
		return "", fmt.Errorf("building the provider for this host: %w", err)
	}

	root := filepath.Join(providerOutputDir, "mirror")
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	fmt.Printf("provider at %s\n", path)
	return absolute, nil
}

// validateVersion rejects what the artifact layout cannot carry. The leading v is checked because
// the tag the release-please component produces has one and the version inside the artifact must
// not: a zip holding terraform-provider-cargoship_vv0.1.0 is one no client looks for.
func validateVersion(version string) error {
	if version == "" {
		return fmt.Errorf("a version is required: the release version without the leading v, e.g. 0.1.0")
	}
	if strings.HasPrefix(version, "v") {
		return fmt.Errorf("version must not carry a leading v: got %s", version)
	}
	return nil
}

// buildPlatform compiles the provider for one platform and zips it, returning the path of the zip.
func buildPlatform(version, oper, arch string) (string, error) {
	binary := fmt.Sprintf("%s_v%s", providerBinaryBase, version)
	if oper == "windows" {
		binary += ".exe"
	}

	dir := filepath.Join(providerOutputDir, fmt.Sprintf("%s_%s", oper, arch))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	binaryPath := filepath.Join(dir, binary)

	fmt.Printf("building %s %s for %s/%s\n", providerBinaryBase, version, oper, arch)
	env := map[string]string{
		"GOOS":        oper,
		"GOARCH":      arch,
		"CGO_ENABLED": "0",
	}
	// The version is stamped into both the command and the provider package: OpenTofu reports
	// the first in `tofu version` and the second in provider metadata, and a provider that says
	// "dev" to either is one nobody can tell apart from a local build.
	ldflags := fmt.Sprintf("-s -w -X main.version=%s -X %s.Version=%s", version, providerImportPath, version)
	if err := sh.RunWithV(env, "go", "build",
		"-trimpath",
		"-ldflags", ldflags,
		"-o", binaryPath,
		providerPackage,
	); err != nil {
		return "", fmt.Errorf("building the provider for %s/%s: %w", oper, arch, err)
	}

	archive := filepath.Join(providerOutputDir, fmt.Sprintf("%s_%s_%s_%s.zip", providerBinaryBase, version, oper, arch))
	if err := zipBinary(archive, binaryPath, binary); err != nil {
		return "", err
	}
	return archive, nil
}

// zipBinary writes a zip holding exactly one entry, the binary, at the root of the archive. That is
// the layout the registry protocol defines and the one OpenTofu unpacks: a nested directory, or a
// second file, is a package it will not read.
func zipBinary(archive, binaryPath, entry string) error {
	f, err := os.Create(archive)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck // the Close below is the one that reports

	w := zip.NewWriter(f)
	header := &zip.FileHeader{
		Name:     entry,
		Method:   zip.Deflate,
		Modified: zipEpoch,
	}
	header.SetMode(0o755)
	entryWriter, err := w.CreateHeader(header)
	if err != nil {
		return err
	}

	binary, err := os.Open(binaryPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(entryWriter, binary); err != nil {
		if closeErr := binary.Close(); closeErr != nil {
			return closeErr
		}
		return err
	}
	if err := binary.Close(); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return f.Close()
}

// pushPlatform pushes one platform's zip and its manifest into the local OCI layout store.
// It returns the descriptor of the platform manifest, configured with its platform and artifactType.
func pushPlatform(ctx context.Context, store *oci.Store, manifestTag, oper, arch, archive string) (ocispec.Descriptor, error) {
	archiveBytes, err := os.ReadFile(archive)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("reading archive %s: %w", archive, err)
	}

	layerDesc := ocispec.Descriptor{
		MediaType: layerMediaType,
		Digest:    digest.FromBytes(archiveBytes),
		Size:      int64(len(archiveBytes)),
		Annotations: map[string]string{
			ocispec.AnnotationTitle: filepath.Base(archive),
		},
	}
	if err := store.Push(ctx, layerDesc, bytes.NewReader(archiveBytes)); err != nil && !errors.Is(err, errdef.ErrAlreadyExists) {
		return ocispec.Descriptor{}, fmt.Errorf("pushing layer for %s_%s: %w", oper, arch, err)
	}

	configDesc := ocispec.DescriptorEmptyJSON
	if err := store.Push(ctx, configDesc, bytes.NewReader(configDesc.Data)); err != nil && !errors.Is(err, errdef.ErrAlreadyExists) {
		return ocispec.Descriptor{}, fmt.Errorf("pushing empty config for %s_%s: %w", oper, arch, err)
	}

	manifest := ocispec.Manifest{
		MediaType:    ocispec.MediaTypeImageManifest,
		ArtifactType: artifactTypeTarget,
		Config:       configDesc,
		Layers:       []ocispec.Descriptor{layerDesc},
	}
	manifest.SchemaVersion = 2
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("marshaling manifest for %s_%s: %w", oper, arch, err)
	}

	manifestDesc := ocispec.Descriptor{
		MediaType:    ocispec.MediaTypeImageManifest,
		Digest:       digest.FromBytes(manifestBytes),
		Size:         int64(len(manifestBytes)),
		ArtifactType: artifactTypeTarget,
		Platform: &ocispec.Platform{
			OS:           oper,
			Architecture: arch,
		},
	}
	if err := store.Push(ctx, manifestDesc, bytes.NewReader(manifestBytes)); err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("pushing manifest for %s_%s: %w", oper, arch, err)
	}
	if err := store.Tag(ctx, manifestDesc, manifestTag); err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("tagging manifest %s: %w", manifestTag, err)
	}
	return manifestDesc, nil
}

// assembleIndex creates an OCI image index pointing to all platform manifests and saves it to store.
func assembleIndex(ctx context.Context, store *oci.Store, tag string, manifests []ocispec.Descriptor) error {
	index := ocispec.Index{
		MediaType:    ocispec.MediaTypeImageIndex,
		ArtifactType: artifactTypeIndex,
		Manifests:    manifests,
	}
	index.SchemaVersion = 2
	indexBytes, err := json.Marshal(index)
	if err != nil {
		return fmt.Errorf("marshaling index: %w", err)
	}

	indexDesc := ocispec.Descriptor{
		MediaType:    ocispec.MediaTypeImageIndex,
		Digest:       digest.FromBytes(indexBytes),
		Size:         int64(len(indexBytes)),
		ArtifactType: artifactTypeIndex,
	}
	if err := store.Push(ctx, indexDesc, bytes.NewReader(indexBytes)); err != nil {
		return fmt.Errorf("pushing index: %w", err)
	}
	if err := store.Tag(ctx, indexDesc, tag); err != nil {
		return fmt.Errorf("tagging index %s: %w", tag, err)
	}
	return nil
}

// pushIndexToRemote copies the tagged index and its dependencies from the local store to repository.
func pushIndexToRemote(ctx context.Context, store *oci.Store, repository, tag string) error {
	repo, err := remote.NewRepository(repository)
	if err != nil {
		return fmt.Errorf("creating remote repository client for %s: %w", repository, err)
	}

	storeOpts := credentials.StoreOptions{
		DetectDefaultNativeStore: true,
	}
	credStore, err := credentials.NewStoreFromDocker(storeOpts)
	if err != nil {
		return fmt.Errorf("creating docker credential store: %w", err)
	}
	repo.Client = &auth.Client{
		Credential: credentials.Credential(credStore),
	}

	_, err = oras.Copy(ctx, store, tag, repo, tag, oras.DefaultCopyOptions)
	if err != nil {
		return fmt.Errorf("copying index %s to %s: %w", tag, repository, err)
	}
	return nil
}
