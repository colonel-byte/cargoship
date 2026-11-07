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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/colonel-byte/cargoship/pkg/helpers"
	"github.com/mholt/archives"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/pkg/archive"
	"github.com/zarf-dev/zarf/src/pkg/oci"
	"github.com/zarf-dev/zarf/src/types"
	"oras.land/oras-go/v2/content"
)

// FileSelector narrows an OCI artifact to the one file a caller wants out of it.
//
// A reference alone frequently does not name bytes. A vendor-published artifact is commonly an
// image index over one manifest per platform, and a manifest can carry one layer or fifty --
// so the architecture picks the manifest, the layer title picks the layer, and an extract path
// picks a member out of a layer that is itself an archive.
type FileSelector struct {
	// Arch is the platform architecture to resolve, for a reference that is an image index.
	// It may be empty for an artifact published as a single manifest.
	Arch string
	// Layer names a layer by its org.opencontainers.image.title annotation. It is required
	// when the manifest carries more than one layer.
	Layer string
	// ExtractPath is a path inside the selected layer, for a layer that is an archive. The
	// layer must carry a title, because the archive format is identified from its file name.
	ExtractPath string
}

// FileOptions is the registry transport and caching an artifact is read with.
type FileOptions struct {
	types.RemoteOptions
	// CachePath roots the shared blob cache, so an artifact's layers are fetched once per
	// cache rather than once per read. Caching is skipped when it is empty.
	CachePath string
}

// A caller resolves these two by supplying something the reference does not carry, and each
// caller spells that differently -- a distro definition has selector.arch and layer fields, the
// sha256sum command has --arch and --layer -- so the error says what is missing and leaves the
// caller to say where to put it.
var (
	// ErrArchRequired is returned when a reference resolves to an image index covering more
	// than one architecture and the selector named none.
	ErrArchRequired = errors.New("no single architecture was selected")
	// ErrLayerRequired is returned when the resolved manifest carries more than one layer and
	// the selector named no layer title.
	ErrLayerRequired = errors.New("no layer title was selected")
)

// maxReportedLayerTitles caps how many titles an error lists. A per-file artifact can carry
// fifty, and an error nobody reads to the end names nothing.
const maxReportedLayerTitles = 8

// FetchFile writes the file ref and sel name to dst, creating dst's parent as needed.
//
// The layer is verified against the digest its manifest gives it as it is read, so a truncated
// or substituted transfer fails here whatever the caller does afterwards.
func FetchFile(ctx context.Context, ref string, sel FileSelector, dst string, opts FileOptions) (err error) {
	remote, err := fileRemote(ctx, ref, sel.Arch, opts)
	if err != nil {
		return err
	}

	manifest, err := resolveManifest(ctx, remote, ref, sel.Arch)
	if err != nil {
		return err
	}
	layer, err := selectLayer(ref, sel, manifest)
	if err != nil {
		return err
	}

	// Fetched through remote.Fetch rather than FetchLayerReader so that the body is closed:
	// the blob cache writes its copy as the reader is drained and finishes that write on
	// Close, and FetchLayerReader's VerifyReader has no Close to call.
	body, err := remote.Fetch(ctx, layer)
	if err != nil {
		return fmt.Errorf("unable to fetch %s from %s: %w", layer.Digest, ref, err)
	}
	defer func() {
		err = errors.Join(err, body.Close())
	}()
	rc := content.NewVerifyReader(body, layer)

	if sel.ExtractPath == "" {
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
		return fmt.Errorf("an extract path was given, but the layer selected in %s carries no %s annotation, so the archive format cannot be identified", ref, ocispec.AnnotationTitle)
	}
	staged, err := os.MkdirTemp("", "cargoship-oci-file-")
	if err != nil {
		return fmt.Errorf("unable to create a staging directory for %s: %w", ref, err)
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

	// Extracted into a directory of its own rather than beside dst, because the member path
	// can carry directories that would otherwise collide with dst itself -- zarf's component
	// tarballs hold "zarf-injector/files/0/zarf-injector" for a target named zarf-injector.
	extractDir := filepath.Join(staged, "extracted")
	if err := archive.Decompress(ctx, stagedArchive, extractDir, archive.DecompressOpts{
		Files: []string{sel.ExtractPath},
	}); err != nil {
		return fmt.Errorf("unable to extract %s from %s: %w", sel.ExtractPath, title, err)
	}
	return helpers.CreatePathAndCopy(filepath.Join(extractDir, filepath.FromSlash(sel.ExtractPath)), dst)
}

// fileRemote opens the registry client an artifact is read through.
func fileRemote(ctx context.Context, ref string, arch string, opts FileOptions) (*Remote, error) {
	mods := []oci.Modifier{
		oci.WithPlainHTTP(opts.PlainHTTP),
		oci.WithInsecureSkipVerify(opts.InsecureSkipTLSVerify),
	}
	if opts.CachePath != "" {
		cacheMod, err := GetOCICacheModifier(ctx, opts.CachePath)
		if err != nil {
			return nil, fmt.Errorf("unable to open the layer cache for %s: %w", ref, err)
		}
		mods = append(mods, cacheMod)
	}

	// The platform passed here is not what selects a child -- resolveManifest does that, for
	// the reason given on acceptedPlatformOS. It is set anyway because an OrasRemote is built
	// with one, and the value only matters to callers that resolve the reference through it.
	remote, err := NewRemote(ctx, ref, ocispec.Platform{Architecture: arch}, mods...)
	if err != nil {
		return nil, fmt.Errorf("unable to read %s: %w", ref, err)
	}
	return remote, nil
}

// ArtifactPlatform is one architecture an OCI artifact offers, and the platform its index
// declares for it.
type ArtifactPlatform struct {
	// Arch is the architecture, which is what a caller selects by.
	Arch string
	// OS is how the artifact spells the operating system -- "linux" for a vendor bundle,
	// "multi" for a zarf or cargoship package index. See acceptedPlatformOS.
	OS string
}

// FileArches lists the architectures ref offers, sorted, for a reference resolving to an image
// index. It returns nothing for an artifact published as a single manifest, which needs no
// architecture to read.
//
// Only children a linux node could use are listed, so this answers the same question
// FetchFile's selection asks. It exists for shell completion, which is why it reads nothing
// but the index: a caller offering --arch suggestions should not pull a layer to do it.
func FileArches(ctx context.Context, ref string, opts FileOptions) ([]ArtifactPlatform, error) {
	remote, err := fileRemote(ctx, ref, "", opts)
	if err != nil {
		return nil, err
	}
	repo := remote.Repo()
	root, err := repo.Resolve(ctx, repo.Reference.Reference)
	if err != nil {
		return nil, fmt.Errorf("unable to resolve %s: %w", ref, err)
	}
	if !isImageIndex(root) {
		return nil, nil
	}
	index, err := fetchIndex(ctx, remote, root)
	if err != nil {
		return nil, fmt.Errorf("unable to read the index of %s: %w", ref, err)
	}

	seen := map[string]bool{}
	var out []ArtifactPlatform
	for _, child := range index.Manifests {
		if child.Platform == nil || child.Platform.Architecture == "" {
			continue
		}
		if !acceptedPlatformOS[child.Platform.OS] || seen[child.Platform.Architecture] {
			continue
		}
		seen[child.Platform.Architecture] = true
		out = append(out, ArtifactPlatform{Arch: child.Platform.Architecture, OS: child.Platform.OS})
	}
	slices.SortFunc(out, func(a, b ArtifactPlatform) int { return strings.Compare(a.Arch, b.Arch) })
	return out, nil
}

// LayerListing is the titles an artifact's manifest carries, and which manifest they came from.
type LayerListing struct {
	// Titles is every titled layer of the manifest read, sorted.
	Titles []string
	// Arch is the architecture whose manifest was read. It is empty for an artifact published
	// as a single manifest, which has no architecture to report.
	Arch string
	// Assumed is true when no architecture was asked for, the reference turned out to be an
	// index over several, and one was read anyway. The titles are then a good guess rather
	// than an answer -- see FileLayers.
	Assumed bool
}

// FileLayers lists the layer titles ref offers for arch.
//
// It exists for shell completion, and its contract is looser than FetchFile's on purpose. With
// no architecture given and a reference that is an index over several, FetchFile refuses --
// there is no safe way to pick bytes. Here the first candidate child is read instead and
// Assumed is set, because every artifact this has been used against titles its layers
// identically across platforms, and a completion that offers nothing until another flag is
// typed teaches an operator nothing. The caller is expected to say that it guessed.
//
// Nothing is decided by this: a title it suggests is still resolved for real, against the
// architecture actually requested, when the file is fetched.
func FileLayers(ctx context.Context, ref string, arch string, opts FileOptions) (LayerListing, error) {
	remote, err := fileRemote(ctx, ref, arch, opts)
	if err != nil {
		return LayerListing{}, err
	}
	repo := remote.Repo()
	root, err := repo.Resolve(ctx, repo.Reference.Reference)
	if err != nil {
		return LayerListing{}, fmt.Errorf("unable to resolve %s: %w", ref, err)
	}

	listing := LayerListing{Arch: arch}
	desc := root
	if isImageIndex(root) {
		index, err := fetchIndex(ctx, remote, root)
		if err != nil {
			return LayerListing{}, fmt.Errorf("unable to read the index of %s: %w", ref, err)
		}
		child, assumed, err := layerListingChild(ref, index, arch)
		if err != nil {
			return LayerListing{}, err
		}
		desc = child
		listing.Assumed = assumed
		if child.Platform != nil {
			listing.Arch = child.Platform.Architecture
		}
	}

	manifest, err := remote.FetchManifest(ctx, desc)
	if err != nil {
		return LayerListing{}, fmt.Errorf("unable to read the manifest of %s: %w", ref, err)
	}
	for _, layer := range manifest.Layers {
		if title := layer.Annotations[ocispec.AnnotationTitle]; title != "" {
			listing.Titles = append(listing.Titles, title)
		}
	}
	slices.Sort(listing.Titles)
	return listing, nil
}

// layerListingChild picks the index child to read titles from, reporting whether it was
// assumed rather than asked for.
func layerListingChild(ref string, index ocispec.Index, arch string) (ocispec.Descriptor, bool, error) {
	var candidates []ocispec.Descriptor
	for _, child := range index.Manifests {
		if child.Platform == nil || !acceptedPlatformOS[child.Platform.OS] {
			continue
		}
		if arch != "" && child.Platform.Architecture != arch {
			continue
		}
		candidates = append(candidates, child)
	}
	if len(candidates) == 0 {
		if arch == "" {
			return ocispec.Descriptor{}, false, fmt.Errorf("%s offers no manifest a linux node can use; it holds %s", ref, indexArches(index))
		}
		return ocispec.Descriptor{}, false, fmt.Errorf("%s holds no %s manifest; it holds %s", ref, arch, indexArches(index))
	}
	// Sorted so that the assumed child is the same one on every invocation: a completion that
	// suggested different titles run to run would be worse than one that suggested none.
	slices.SortFunc(candidates, func(a, b ocispec.Descriptor) int {
		return strings.Compare(a.Platform.Architecture, b.Platform.Architecture)
	})
	return candidates[0], arch == "" && len(index.Manifests) > 1, nil
}

// FileMembers lists the paths inside the layer ref and sel select, for a layer that is an
// archive. The result is what --extract-path or a file entry's extractPath can name.
//
// Unlike FileArches and FileLayers this reads the layer itself rather than a manifest, which
// for a vendor engine bundle is tens of mebibytes. Two things make that acceptable rather than
// rude, and both are why the caller should only ask once a layer has actually been chosen:
//
//   - it is exactly the blob the command being completed is about to fetch anyway, into the
//     same cache, so a completion that is followed through costs nothing extra; and
//   - a second ask is served from that cache without touching the network.
//
// Pass a context with a deadline. A caller offering suggestions must not block, and an
// abandoned transfer is dropped rather than cached: the blob is verified against its digest
// before the cache commits it.
func FileMembers(ctx context.Context, ref string, sel FileSelector, opts FileOptions) (_ []string, err error) {
	remote, err := fileRemote(ctx, ref, sel.Arch, opts)
	if err != nil {
		return nil, err
	}
	manifest, err := resolveManifest(ctx, remote, ref, sel.Arch)
	if err != nil {
		return nil, err
	}
	layer, err := selectLayer(ref, sel, manifest)
	if err != nil {
		return nil, err
	}
	title := layer.Annotations[ocispec.AnnotationTitle]
	if title == "" {
		return nil, fmt.Errorf("the layer selected in %s carries no %s annotation, so the archive format cannot be identified", ref, ocispec.AnnotationTitle)
	}

	body, err := remote.Fetch(ctx, layer)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch %s from %s: %w", layer.Digest, ref, err)
	}
	defer func() {
		err = errors.Join(err, body.Close())
	}()

	// Identified from the layer's title, the way the extraction in FetchFile is, so a layer
	// this lists members of is one that can actually be extracted from.
	format, stream, err := archives.Identify(ctx, filepath.Base(filepath.FromSlash(title)), content.NewVerifyReader(body, layer))
	if err != nil {
		return nil, fmt.Errorf("unable to identify %s as an archive: %w", title, err)
	}
	extractor, ok := format.(archives.Extractor)
	if !ok {
		return nil, fmt.Errorf("%s is a %T, which holds no members to extract", title, format)
	}

	var members []string
	err = extractor.Extract(ctx, stream, func(_ context.Context, f archives.FileInfo) error {
		if !f.IsDir() {
			members = append(members, f.NameInArchive)
		}
		return nil
	})
	// A deadline reached part-way through still leaves the members read so far, which is a
	// better answer for a completion than none at all.
	if err != nil && len(members) == 0 {
		return nil, fmt.Errorf("unable to read the members of %s: %w", title, err)
	}
	slices.Sort(members)
	return slices.Compact(members), nil
}

// acceptedPlatformOS is the set of operating systems a file artifact's platform child may name
// and still be a candidate for a linux cluster.
//
// Three spellings are in circulation and a read has to handle all of them, which is why the
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
// candidate, because what cargoship reads out of these is going to a linux node.
var acceptedPlatformOS = map[string]bool{"": true, "linux": true, "multi": true}

// resolveManifest resolves the reference to the one manifest arch selects, descending an image
// index when the reference is one.
//
// The descent is done here rather than left to oras.Resolve so that every OS spelling above is
// readable, and so that a reference naming several architectures fails with a message that
// names them -- oras reports only that nothing matched.
func resolveManifest(ctx context.Context, remote *Remote, ref string, arch string) (*oci.Manifest, error) {
	repo := remote.Repo()
	root, err := repo.Resolve(ctx, repo.Reference.Reference)
	if err != nil {
		return nil, fmt.Errorf("unable to resolve %s: %w", ref, err)
	}
	if !isImageIndex(root) {
		manifest, err := remote.FetchManifest(ctx, root)
		if err != nil {
			return nil, fmt.Errorf("unable to read the manifest of %s: %w", ref, err)
		}
		return manifest, nil
	}

	index, err := fetchIndex(ctx, remote, root)
	if err != nil {
		return nil, fmt.Errorf("unable to read the index of %s: %w", ref, err)
	}

	// An index over one manifest names bytes whatever the caller selects, so no architecture
	// is only ambiguous when there is something to be ambiguous about.
	if arch == "" {
		if len(index.Manifests) == 1 {
			manifest, err := remote.FetchManifest(ctx, index.Manifests[0])
			if err != nil {
				return nil, fmt.Errorf("unable to read the manifest of %s: %w", ref, err)
			}
			return manifest, nil
		}
		return nil, fmt.Errorf("%s is an index over %s: %w", ref, indexArches(index), ErrArchRequired)
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
		return nil, fmt.Errorf("%s holds no %s manifest; it holds %s", ref, arch, indexArches(index))
	case 1:
		manifest, err := remote.FetchManifest(ctx, matched[0])
		if err != nil {
			return nil, fmt.Errorf("unable to read the %s manifest of %s: %w", arch, ref, err)
		}
		return manifest, nil
	default:
		return nil, fmt.Errorf("%s holds %d manifests for %s, so the reference and that architecture together still do not name one file", ref, len(matched), arch)
	}
}

// mediaTypeDockerManifestList is the Docker spelling of an image index.
const mediaTypeDockerManifestList = "application/vnd.docker.distribution.manifest.list.v2+json"

// isImageIndex reports whether desc describes an image index, in either spelling.
func isImageIndex(desc ocispec.Descriptor) bool {
	return desc.MediaType == ocispec.MediaTypeImageIndex || desc.MediaType == mediaTypeDockerManifestList
}

// fetchIndex reads an index descriptor's bytes and unmarshals them.
func fetchIndex(ctx context.Context, remote *Remote, desc ocispec.Descriptor) (_ ocispec.Index, err error) {
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

// selectLayer picks the layer a selector means out of an artifact's manifest.
//
// Two shapes are in use and they are resolved differently, which is why the caller says which
// it is rather than this guessing. A vendor commonly publishes one tarball per platform as a
// single titled layer, where the member inside it is named by an extract path; others publish
// every file in the release as its own titled layer, where the title names it. See
// docs/agent/choice-oci-file-source.md.
func selectLayer(ref string, sel FileSelector, manifest *oci.Manifest) (ocispec.Descriptor, error) {
	if sel.Layer != "" {
		desc := manifest.Locate(sel.Layer)
		if oci.IsEmptyDescriptor(desc) {
			return ocispec.Descriptor{}, fmt.Errorf("%s carries no layer titled %q; it carries %s", ref, sel.Layer, layerTitles(manifest))
		}
		return desc, nil
	}
	if len(manifest.Layers) == 1 {
		return manifest.Layers[0], nil
	}
	return ocispec.Descriptor{}, fmt.Errorf("%s carries %d layers, so the reference alone does not name one file and %w; it carries %s",
		ref, len(manifest.Layers), ErrLayerRequired, layerTitles(manifest))
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
