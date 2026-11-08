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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/pkg/coci"
	"github.com/colonel-byte/cargoship/pkg/helpers"
	"github.com/colonel-byte/cargoship/pkg/images"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/pkg/archive"
	"github.com/zarf-dev/zarf/src/pkg/oci"
	"github.com/zarf-dev/zarf/src/types"
	"oras.land/oras-go/v2/content"
)

// ociOptions is what an oci:// file source needs from the build's options, which fileGrabber
// does not otherwise receive. It is one struct rather than four arguments because every one of
// them is a property of the build and none of them is a property of the file.
type ociOptions struct {
	types.RemoteOptions
	// CachePath roots the shared blob cache, so an artifact's layers are fetched once per
	// cache rather than once per build. A 43 MiB engine tarball is a slow thing to fetch
	// twice.
	CachePath string
	// RegistryOverrides redirect the fetch by registry prefix, the same map image pulls use.
	RegistryOverrides []images.RegistryOverride
}

// maxReportedLayerTitles caps how many titles an error lists. A per-file artifact can carry
// fifty, and an error nobody reads to the end names nothing.
const maxReportedLayerTitles = 8

// fetchOCIFile stages the bytes an oci:// file source names, writing them to dst, or -- when
// the entry declares an extractPath -- extracting that member out of them into destinationDir.
//
// src is the reference to fetch, which is not necessarily file.Source: a registry override may
// have rewritten it. file is still the entry as the definition wrote it, because that is what
// the messages should name.
func fetchOCIFile(ctx context.Context, src string, file v1alpha1.File, arch string, dst string, destinationDir string, opts ociOptions) (err error) {
	mods := []oci.Modifier{
		oci.WithPlainHTTP(opts.PlainHTTP),
		oci.WithInsecureSkipVerify(opts.InsecureSkipTLSVerify),
	}
	if opts.CachePath != "" {
		cacheMod, err := coci.GetOCICacheModifier(ctx, opts.CachePath)
		if err != nil {
			return fmt.Errorf("unable to open the layer cache for %s: %w", file.Source, err)
		}
		mods = append(mods, cacheMod)
	}

	// The platform passed here is not what selects a child -- resolveManifest does that
	// below, for the reason given on acceptedPlatformOS. It is set anyway because an
	// OrasRemote is built with one, and the value only matters to callers that resolve the
	// reference through it.
	remote, err := coci.NewRemote(ctx, src, ocispec.Platform{Architecture: arch}, mods...)
	if err != nil {
		return fmt.Errorf("unable to read %s: %w", file.Source, err)
	}

	manifest, err := resolveManifest(ctx, remote, file, arch)
	if err != nil {
		return err
	}

	layer, err := ociFileLayer(file, manifest)
	if err != nil {
		return err
	}

	// Fetched through remote.Fetch rather than FetchLayerReader so that the body is closed:
	// the blob cache writes its copy as the reader is drained and finishes that write on
	// Close, and FetchLayerReader's VerifyReader has no Close to call.
	body, err := remote.Fetch(ctx, layer)
	if err != nil {
		return fmt.Errorf("unable to fetch %s from %s: %w", layer.Digest, file.Source, err)
	}
	defer func() {
		err = errors.Join(err, body.Close())
	}()
	// Verified against the descriptor as it is read, so what lands on disk is what the
	// manifest claims whether or not the entry declares a shasum of its own.
	rc := content.NewVerifyReader(body, layer)

	if file.ExtractPath == "" {
		if err := writeFileFrom(rc, dst); err != nil {
			return fmt.Errorf("unable to write %s: %w", dst, err)
		}
		return rc.Verify()
	}

	// The layer is itself an archive, and the extraction identifies the format from the file
	// name rather than from the bytes -- so the staged copy has to keep the name the artifact
	// gave the layer, extension and all.
	title := layer.Annotations[ocispec.AnnotationTitle]
	if title == "" {
		return fmt.Errorf("%s declares extractPath, but the layer it selects in %s carries no %s annotation, so the archive format cannot be identified", file.String(), file.Source, ocispec.AnnotationTitle)
	}
	staged, err := os.MkdirTemp("", "cargoship-oci-file-")
	if err != nil {
		return fmt.Errorf("unable to create a staging directory for %s: %w", file.Source, err)
	}
	defer func() {
		err = errors.Join(err, os.RemoveAll(staged))
	}()
	stagedArchive := filepath.Join(staged, filepath.Base(filepath.FromSlash(title)))
	if err := writeFileFrom(rc, stagedArchive); err != nil {
		return fmt.Errorf("unable to stage %s: %w", title, err)
	}
	if err := rc.Verify(); err != nil {
		return err
	}

	if err := archive.Decompress(ctx, stagedArchive, destinationDir, archive.DecompressOpts{
		Files: []string{file.ExtractPath},
	}); err != nil {
		return fmt.Errorf("unable to extract %s from %s: %w", file.ExtractPath, title, err)
	}
	return nil
}

// acceptedPlatformOS is the set of operating systems a file artifact's platform child may name
// and still be a candidate for a linux cluster.
//
// Three spellings are in circulation and a build has to read all of them, which is why the
// child is selected here rather than by handing a platform to oras:
//
//   - "linux" -- what a vendor publishing real binaries writes, and what pkg/images asks for
//     when it pulls an image. ghcr.io/cri-o/bundle and the Rancher Carbide rke2 artifacts are
//     this shape.
//   - "multi" -- what zarf and cargoship write on a package index, whose children are
//     architectures of a package rather than container platforms. ghcr.io/zarf-dev/packages/init
//     is this, so matching "linux" alone cannot read a zarf init package at all.
//   - "" -- an artifact recording an architecture and no OS.
//
// Matching on the architecture and accepting any of these is deliberate: the alternative is
// knowing which kind of publisher produced an artifact before reading it, which is not
// something the reference says. A child naming any other OS -- windows, darwin -- is not a
// candidate, because what cargoship stages is going to a linux node.
var acceptedPlatformOS = map[string]bool{"": true, "linux": true, "multi": true}

// resolveManifest resolves the reference to the one manifest an entry's architecture selects,
// descending an image index when the reference is one.
//
// The descent is done here rather than left to oras.Resolve so that every OS spelling above is
// readable, and so that a reference naming several architectures fails with a message that
// names them and points at selector.arch -- oras reports only that nothing matched.
func resolveManifest(ctx context.Context, remote *coci.Remote, file v1alpha1.File, arch string) (*oci.Manifest, error) {
	repo := remote.Repo()
	root, err := repo.Resolve(ctx, repo.Reference.Reference)
	if err != nil {
		return nil, fmt.Errorf("unable to resolve %s: %w", file.Source, err)
	}
	if !isImageIndex(root) {
		manifest, err := remote.FetchManifest(ctx, root)
		if err != nil {
			return nil, fmt.Errorf("unable to read the manifest of %s: %w", file.Source, err)
		}
		return manifest, nil
	}

	index, err := fetchIndex(ctx, remote, root)
	if err != nil {
		return nil, fmt.Errorf("unable to read the index of %s: %w", file.Source, err)
	}

	// An index over one manifest names bytes whatever the entry selects, so an entry with no
	// architecture is only ambiguous when there is something to be ambiguous about.
	if arch == "" {
		if len(index.Manifests) == 1 {
			manifest, err := remote.FetchManifest(ctx, index.Manifests[0])
			if err != nil {
				return nil, fmt.Errorf("unable to read the manifest of %s: %w", file.Source, err)
			}
			return manifest, nil
		}
		return nil, fmt.Errorf("%s is an index over %s, and the file entry selects no single architecture: name one in its selector.arch",
			file.Source, indexArches(index))
	}

	var matched []ocispec.Descriptor
	for _, child := range index.Manifests {
		if child.Platform == nil || child.Platform.Architecture != arch {
			continue
		}
		if !acceptedPlatformOS[child.Platform.OS] {
			continue
		}
		matched = append(matched, child)
	}
	switch len(matched) {
	case 0:
		return nil, fmt.Errorf("%s holds no %s manifest; it holds %s", file.Source, arch, indexArches(index))
	case 1:
		manifest, err := remote.FetchManifest(ctx, matched[0])
		if err != nil {
			return nil, fmt.Errorf("unable to read the %s manifest of %s: %w", arch, file.Source, err)
		}
		return manifest, nil
	default:
		return nil, fmt.Errorf("%s holds %d manifests for %s, so the reference and the entry's selector.arch together still do not name one file", file.Source, len(matched), arch)
	}
}

// mediaTypeDockerManifestList is the Docker spelling of an image index.
const mediaTypeDockerManifestList = "application/vnd.docker.distribution.manifest.list.v2+json"

// isImageIndex reports whether desc describes an image index, in either spelling.
func isImageIndex(desc ocispec.Descriptor) bool {
	return desc.MediaType == ocispec.MediaTypeImageIndex || desc.MediaType == mediaTypeDockerManifestList
}

// fetchIndex reads an index descriptor's bytes and unmarshals them.
func fetchIndex(ctx context.Context, remote *coci.Remote, desc ocispec.Descriptor) (_ ocispec.Index, err error) {
	body, err := remote.Fetch(ctx, desc)
	if err != nil {
		return ocispec.Index{}, err
	}
	defer func() {
		err = errors.Join(err, body.Close())
	}()
	b, err := io.ReadAll(content.NewVerifyReader(body, desc))
	if err != nil {
		return ocispec.Index{}, err
	}
	var index ocispec.Index
	if err := json.Unmarshal(b, &index); err != nil {
		return ocispec.Index{}, err
	}
	return index, nil
}

// indexArches spells an index's children for a message, as "amd64, arm64, skeleton". A child
// for an OS a linux cluster cannot use keeps its OS in the list, so that an artifact offering
// only windows reads as such rather than as offering the architecture.
func indexArches(index ocispec.Index) string {
	arches := make([]string, 0, len(index.Manifests))
	for _, child := range index.Manifests {
		switch {
		case child.Platform == nil || child.Platform.Architecture == "":
			arches = append(arches, "an unplatformed manifest")
		case acceptedPlatformOS[child.Platform.OS]:
			arches = append(arches, child.Platform.Architecture)
		default:
			arches = append(arches, child.Platform.OS+"/"+child.Platform.Architecture)
		}
	}
	if len(arches) == 0 {
		return "no manifests"
	}
	slices.Sort(arches)
	return strings.Join(slices.Compact(arches), ", ")
}

// ociFileLayer picks the layer a file entry means out of an artifact's manifest.
//
// Two shapes are in use and they are resolved differently, which is why an entry says which it
// is rather than this guessing. A vendor commonly publishes one tarball per platform as a
// single titled layer, where the entry names a path inside that tarball; others publish every
// file in the release as its own titled layer, where the entry names the title. See
// docs/agent/choice-oci-file-source.md.
func ociFileLayer(file v1alpha1.File, manifest *oci.Manifest) (ocispec.Descriptor, error) {
	if file.Layer != "" {
		desc := manifest.Locate(file.Layer)
		if oci.IsEmptyDescriptor(desc) {
			return ocispec.Descriptor{}, fmt.Errorf("%s carries no layer titled %q; it carries %s", file.Source, file.Layer, layerTitles(manifest))
		}
		return desc, nil
	}
	if len(manifest.Layers) == 1 {
		return manifest.Layers[0], nil
	}
	return ocispec.Descriptor{}, fmt.Errorf("%s carries %d layers, so the reference alone does not name one file: name the one this entry wants in its layer field; it carries %s",
		file.Source, len(manifest.Layers), layerTitles(manifest))
}

// layerTitles lists what an artifact's layers are titled, for an error that would otherwise
// leave a reader to go and look.
func layerTitles(manifest *oci.Manifest) string {
	titles := make([]string, 0, len(manifest.Layers))
	for _, layer := range manifest.Layers {
		if title := layer.Annotations[ocispec.AnnotationTitle]; title != "" {
			titles = append(titles, title)
		}
	}
	if len(titles) == 0 {
		return "no titled layers"
	}
	slices.Sort(titles)
	if len(titles) > maxReportedLayerTitles {
		return fmt.Sprintf("%s and %d more", strings.Join(titles[:maxReportedLayerTitles], ", "), len(titles)-maxReportedLayerTitles)
	}
	return strings.Join(titles, ", ")
}

// writeFileFrom creates dst and copies r into it.
func writeFileFrom(r io.Reader, dst string) (err error) {
	if err := os.MkdirAll(filepath.Dir(dst), helpers.ReadExecuteAllWriteUser); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, out.Close())
	}()
	_, err = io.Copy(out, r)
	return err
}

// pinnedByDigest reports whether ref names content by digest rather than by tag, which makes
// it verify itself: a registry that served different bytes would fail the digest check.
func pinnedByDigest(ref string) bool {
	_, dgst, found := strings.Cut(ref, "@")
	return found && dgst != ""
}

// ociOptions narrows the build's options to what an oci:// file source needs.
func (o AssembleOptions) ociOptions() ociOptions {
	return ociOptions{
		RemoteOptions:     o.RemoteOptions,
		CachePath:         o.CachePath,
		RegistryOverrides: o.RegistryOverrides,
	}
}
