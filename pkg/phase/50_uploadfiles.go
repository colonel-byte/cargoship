// Copyright 2023 k0sctl authors
// Copyright 2026 colonel-byte
//
// This file contains code derived from k0sctl:
// https://github.com/k0sproject/k0sctl
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

package phase

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/colonel-byte/cargoship/api"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/images"
	"github.com/containerd/platforms"
	"github.com/google/go-containerregistry/pkg/name"
	ggcrv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"
)

// UploadFiles implements a phase which upload files to hosts
type UploadFiles struct {
	GenericPhase

	hosts    cluster.Hosts
	disFiles v1alpha1.Files
	// imgFiles holds the image tarballs to upload, keyed by the architecture of the host
	// receiving them. An image pulled for several architectures is exported once per
	// architecture the cluster actually runs, into its own directory under the tarball dir.
	imgFiles map[api.Arch][]v1alpha1.File

	// priorManifest holds each host's upload manifest as it was before this run touched it,
	// captured in Prepare so Run can tell which images an upgrade no longer uploads (e.g. an
	// image tag that was removed from the distro config, or renamed to a new version).
	priorManifest map[*cluster.Host][]ManifestEntry

	// kept records the image tarballs the sweep left on a host because the host asked it to.
	// Written from the parallel sweep, so it is guarded; read once, after the run, by Warnings.
	keptMu sync.Mutex
	kept   []Warning
}

// KeepImagesMarker is the file an operator drops in the engine's image directory to stop
// cargoship from removing image tarballs it did not upload.
//
// The directory is cargoship's to manage -- it is populated almost entirely by the upload phase,
// and a tarball in it that no manifest names is one nothing would ever clean up -- so the sweep
// removes what it does not recognise. That is the wrong answer for a fleet where something else
// also puts images there, and this file is how such a host says so. It changes nothing about the
// images cargoship does own: those are still uploaded, still recorded, and still pruned when the
// package stops carrying them.
const KeepImagesMarker = "cargoship-keep-images"

// imageTarBallSuffixes are the filename endings the sweep treats as an image tarball. They are
// the three ImageConfig.TarballSuffix can produce, rather than only the one this package uses,
// so a package that switches compression leaves no copy of the old file behind.
//
// Anything else in the directory is left alone, which includes the image list files an engine
// reads alongside the tarballs.
var imageTarBallSuffixes = []string{".tar", ".tar.gz", ".tar.zst"}

// isImageTarBall reports whether a filename in the image directory is one of ours to sweep.
func isImageTarBall(name string) bool {
	for _, suffix := range imageTarBallSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// Title for the phase
func (p *UploadFiles) Title() string {
	return "Upload files to hosts"
}

// Explanation about the current phase, used for documentation generation
func (p *UploadFiles) Explanation() string {
	return "Uploads the distro agnostic files and image tarballs to each remote node, and removes image tarballs it did not upload unless the image directory carries a " + KeepImagesMarker + " file"
}

// unsafeTarBallChars matches every character an image reference may carry that does not belong
// in a filename. Each one becomes "_", which keeps the whole reference -- registry port, path and
// tag or digest -- in the tarball's name, so two tags of one repository, or two images from a
// registry with a port, do not resolve to the same path.
var unsafeTarBallChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// imageTarBallName returns the filename an image reference's tarball is exported under.
func imageTarBallName(image, tarSuffix string) string {
	return unsafeTarBallChars.ReplaceAllLiteralString(image, "_") + tarSuffix
}

// Prepare the phase
func (p *UploadFiles) Prepare(ctx context.Context, c *cluster.Cluster, d *distro.Distro) error {
	p.manager.Config = c
	p.hosts = p.manager.Config.Spec.Hosts.Filter(func(h *cluster.Host) bool {
		return (len(h.Files) + len(d.Spec.Config.Files) + len(p.manager.Distro.Spec.Config.EnabledImages())) > 0
	})
	p.disFiles = p.manager.Distro.Spec.Config.Files

	p.priorManifest = make(map[*cluster.Host][]ManifestEntry, len(p.manager.Config.Spec.Hosts))
	for _, h := range p.manager.Config.Spec.Hosts {
		p.priorManifest[h] = p.readManifest(h)
	}

	imagesPath := filepath.Join(p.manager.TempDirectory, config.ImagesDir)
	src, err := oci.NewWithContext(ctx, imagesPath)
	if err != nil {
		return err
	}

	lp, err := layout.FromPath(imagesPath)
	if err != nil {
		return err
	}

	// Tarballs are built for the architectures the cluster actually runs rather than every
	// architecture the package carries, so applying a multi-architecture package to a cluster of
	// one architecture does not pay to export images nobody uploads.
	arches := hostArches(ctx, p.manager.Config.Spec.Hosts)
	p.imgFiles = make(map[api.Arch][]v1alpha1.File, len(arches))

	for _, arch := range arches {
		if err := p.exportImagesForArch(ctx, src, lp, arch); err != nil {
			return err
		}
	}

	return nil
}

// exportImagesForArch writes one image tarball per configured image for a single architecture.
//
// Each architecture gets its own directory under the tarball dir. The tarball name is derived from
// the image reference alone, so two architectures of the same image would otherwise be written to
// the same path, and the second export would overwrite the first.
func (p *UploadFiles) exportImagesForArch(ctx context.Context, src *oci.Store, lp layout.Path, arch api.Arch) error {
	compression := p.manager.Distro.Spec.Config.ImagesConfig.Compression
	tarSuffix, err := p.manager.Distro.Spec.Config.ImagesConfig.TarballSuffix()
	if err != nil {
		return err
	}

	exportPlatform := imageExportPlatform(arch)

	tarBallDir := filepath.Join(p.manager.TempDirectory, config.TarBallDir, string(arch))
	if err := os.MkdirAll(tarBallDir, 0755); err != nil {
		return err
	}

	// shortcut: follows the chart entries' enabled key and not the engine's disable list, so
	// a chart disabled through addons.disabled still has its images uploaded; upgrade when
	// enabled and disable are reconciled -- see docs/agent/todo-chart-disable-image-gating.md
	//
	// EnabledImages, not imageConfig.images: a chart manifest the cluster disabled keeps
	// its images in the package and has none of them exported or uploaded. Enablement is
	// a property of the cluster rather than of a host, so it is resolved once here and
	// the per-arch keying below is unaffected.
	for _, i := range p.manager.Distro.Spec.Config.EnabledImages() {
		tarBallName := imageTarBallName(i, tarSuffix)
		tarballPath := filepath.Join(tarBallDir, tarBallName)

		tagged, err := src.Resolve(ctx, i)
		if err != nil {
			return err
		}

		desc, err := resolveImageManifest(ctx, src, tagged, exportPlatform)
		if err != nil {
			return fmt.Errorf("failed to select a %s manifest for image %s: %w", arch, i, err)
		}

		img, err := layoutImage(lp, tagged, desc)
		if err != nil {
			return fmt.Errorf("failed to open the %s manifest for image %s: %w", arch, i, err)
		}

		ref, err := name.ParseReference(i)
		if err != nil {
			return fmt.Errorf("failed to parse image reference %s: %w", i, err)
		}

		// O_TRUNC, not O_APPEND: appending to a tarball that is already there concatenates a
		// second archive onto the first rather than replacing it.
		writer, err := os.OpenFile(tarballPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}

		compressor, err := imageCompressWriter(compression, writer)
		if err != nil {
			if cerr := writer.Close(); cerr != nil {
				logger.From(ctx).Warn("failed to close writer", "error", cerr)
			}
			return err
		}

		if err := tarball.Write(ref, img, compressor); err != nil {
			logger.From(ctx).Warn("failed to create archive", "error", err)
		}

		// The compressor has to be closed before the file so its trailer lands in the tarball.
		err = compressor.Close()
		if err != nil {
			logger.From(ctx).Warn("failed to close compressor", "compression", compression, "error", err)
		}

		err = writer.Close()
		if err != nil {
			logger.From(ctx).Warn("failed to close writer", "error", err)
		}

		err = os.Chtimes(tarballPath, time.Unix(0, 0), time.Unix(0, 0))
		if err != nil {
			return err
		}

		p.imgFiles[arch] = append(p.imgFiles[arch], v1alpha1.File{
			Name:        tarBallName,
			Target:      p.manager.Distro.Spec.Config.ImagesConfig.Path,
			TargetIsDir: true,
			Category:    "image",
			LocalSource: v1alpha1.LocalFile{
				Path: tarballPath,
			},
		})
	}

	return nil
}

// imageExportOS is the operating system of every platform cargoship exports an image tarball for.
// The tarballs are imported by an engine agent on a Linux host, so the OS never follows the machine
// running cargoship, which may well be macOS.
const imageExportOS = "linux"

// imageExportPlatform returns the matcher used to pick and export image manifests for one host
// architecture.
//
// The architecture comes from the cluster rather than from platforms.DefaultStrict(), which
// describes the machine running cargoship. Selecting against the workstation writes a tarball no
// node in the cluster can use as soon as the two differ.
func imageExportPlatform(arch api.Arch) platforms.MatchComparer {
	return platforms.OnlyStrict(ocispec.Platform{
		OS:           imageExportOS,
		Architecture: string(arch),
	})
}

// ggcrHash converts an OCI descriptor digest into the hash type go-containerregistry addresses
// blobs by.
func ggcrHash(d digest.Digest) ggcrv1.Hash {
	return ggcrv1.Hash{Algorithm: d.Algorithm().String(), Hex: d.Encoded()}
}

// layoutImage opens the manifest chosen for an architecture out of the OCI layout the images were
// pulled into. tagged is the descriptor the image reference resolves to and resolved the manifest
// picked out of it, which are the same descriptor for a single-architecture image.
//
// layout.Path resolves a digest against index.json alone and does not recurse, so a manifest that
// is a child of a tagged index has to be reached through its parent rather than looked up directly.
func layoutImage(lp layout.Path, tagged, resolved ocispec.Descriptor) (ggcrv1.Image, error) {
	root, err := lp.ImageIndex()
	if err != nil {
		return nil, err
	}
	if tagged.Digest == resolved.Digest {
		return root.Image(ggcrHash(resolved.Digest))
	}
	parent, err := root.ImageIndex(ggcrHash(tagged.Digest))
	if err != nil {
		return nil, err
	}
	return parent.Image(ggcrHash(resolved.Digest))
}

// resolveImageManifest picks the manifest to export for an image. An image pulled for a single
// architecture is tagged as a manifest and comes back unchanged. An image pulled for several is
// tagged as an index, so the manifest matching the platform is chosen out of it here: a tarball
// carries one image per architecture, and the tarball writer is handed a manifest rather than an
// index so it never writes blobs for a platform this host will not run.
func resolveImageManifest(ctx context.Context, src *oci.Store, desc ocispec.Descriptor, matcher platforms.MatchComparer) (ocispec.Descriptor, error) {
	if !images.IsIndex(desc.MediaType) {
		return desc, nil
	}

	indexBytes, err := content.FetchAll(ctx, src, desc)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("failed to read image index %s: %w", desc.Digest, err)
	}
	var index ocispec.Index
	if err := json.Unmarshal(indexBytes, &index); err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("failed to parse image index %s: %w", desc.Digest, err)
	}

	matches := []ocispec.Descriptor{}
	available := []string{}
	for _, child := range index.Manifests {
		if child.Platform == nil {
			continue
		}
		available = append(available, platforms.Format(*child.Platform))
		if matcher.Match(*child.Platform) {
			matches = append(matches, child)
		}
	}
	if len(matches) == 0 {
		return ocispec.Descriptor{}, fmt.Errorf("image index %s holds no manifest for this platform, it holds %s",
			desc.Digest, strings.Join(available, ", "))
	}
	// An index can hold more than one manifest a platform accepts, for example an arm64 variant the
	// host can run alongside the plain arm64 build, so the matcher orders them by preference.
	sort.SliceStable(matches, func(i, j int) bool {
		return matcher.Less(*matches[i].Platform, *matches[j].Platform)
	})

	return matches[0], nil
}

// ShouldRun is true when there are workers
func (p *UploadFiles) ShouldRun() bool {
	return len(p.hosts) > 0
}

// Run the phase
func (p *UploadFiles) Run(ctx context.Context) error {
	logger.From(ctx).Info("needing to upload files", "count", len(p.disFiles))
	for arch, files := range p.imgFiles {
		logger.From(ctx).Info("needing to upload images", "arch", arch, "count", len(files))
	}

	return p.parallelDoUpload(
		ctx,
		p.manager.Config.Spec.Hosts,
		p.cleanUpOldTmpFiles,
		p.uploadDistroFiles,
		p.cleanStaleUploads,
	)
}

// cleanStaleUploads removes images this run's upload left in the manifest from a previous
// version but didn't re-upload itself, e.g. an image tag bumped by a version upgrade. The diff
// is scoped to the "image" category: the engine binary phases run after this one and haven't
// recorded their own files into h.Metadata.UploadedFiles yet, so comparing the full manifest
// here would misread their files as stale and delete a still-in-use engine binary.
func (p *UploadFiles) cleanStaleUploads(ctx context.Context, h *cluster.Host) error {
	current := ParseManifest(strings.Join(h.Metadata.UploadedFiles, "\n"))
	old := filterManifestByCategory(p.priorManifest[h], "image")
	current = filterManifestByCategory(current, "image")
	p.removeStaleManifestEntries(ctx, h, old, current)
	p.sweepUnmanagedImages(ctx, h, current)
	return nil
}

// sweepUnmanagedImages removes image tarballs in the engine's image directory that this run did
// not upload and the manifest does not name.
//
// The manifest prune above it only reaches what cargoship recorded, which leaves a gap the
// manifest cannot see: a tarball from a version that predates the manifest, one left by a run
// that was interrupted between the upload and the manifest write, or one a previous naming
// scheme wrote under a name nothing resolves any more. Nothing in the manifest would ever
// remove those, and nobody is watching the disk on an air-gapped host.
//
// A host carrying KeepImagesMarker gets the warning instead of the deletion. The images
// cargoship does own are unaffected either way -- they are pruned by the manifest, above.
func (p *UploadFiles) sweepUnmanagedImages(ctx context.Context, h *cluster.Host, current []ManifestEntry) {
	l := logger.From(ctx)

	dir := p.manager.Distro.Spec.Config.ImagesConfig.Path
	if dir == "" {
		return
	}

	entries, err := h.ReadDir(dir)
	if err != nil {
		// A host with no image directory has no unmanaged tarballs in it. Swallowed like
		// recordManifestEntry's failures: this is cleanup, and failing the apply over it would
		// stop an install that has otherwise worked.
		l.Debug("could not list the image directory", "host", h, "path", dir, "error", err)
		return
	}

	managed := make(map[string]struct{}, len(current))
	for _, e := range current {
		managed[e.Path] = struct{}{}
	}

	marker := path.Join(dir, KeepImagesMarker)
	keep := h.FileExist(marker)

	for _, e := range entries {
		if e.IsDir() || !isImageTarBall(e.Name()) {
			continue
		}
		target := path.Join(dir, e.Name())
		if _, ok := managed[target]; ok {
			continue
		}

		if keep {
			l.Warn("keeping an image tarball cargoship did not upload", "host", h, "path", target, "marker", marker)
			p.keptMu.Lock()
			p.kept = append(p.kept, Warning{
				Host:   h.ConfiguredAddress(),
				Path:   target,
				Reason: "kept, unmanaged",
			})
			p.keptMu.Unlock()
			continue
		}

		l.Info("removing an image tarball cargoship did not upload", "host", h, "path", target)
		if err := deleteRecordedUpload(ctx, h, target); err != nil {
			l.Warn("failed to remove an unmanaged image tarball", "host", h, "path", target, "error", err)
		}
	}
}

// Warnings reports the image tarballs the sweep left in place. See warningReporter in
// 05_manager.go: they are repeated in the run's recap because a line logged during the upload
// phase is a long way above the prompt by the time a fleet-sized apply finishes.
func (p *UploadFiles) Warnings() []Warning {
	p.keptMu.Lock()
	defer p.keptMu.Unlock()
	return slices.Clone(p.kept)
}

func (p *UploadFiles) cleanUpOldTmpFiles(ctx context.Context, h *cluster.Host) error {
	l := logger.From(ctx)

	files := slices.Concat(p.disFiles, p.manager.Distro.Spec.Config.OS.Files)

	for _, f := range files {
		file := filepath.Base(f.Target)
		binary := fmt.Sprintf("%s.tmp.*", file)
		re := regexp.MustCompile(binary)
		folder := filepath.Dir(f.Target)
		if f.TargetIsDir {
			folder = f.Target
		}
		err := fs.WalkDir(h.Sudo().FS(), folder, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				l.Debug(fmt.Sprintf("failed to walk %s", binary), "path", file, "error", err)
				return nil
			}
			if !d.IsDir() && re.MatchString(d.Name()) {
				l.Debug("cleaning up old engine binary upload temporary file", "host", h, "path", path)
				if err := h.DeleteFile(path); err != nil {
					l.Warn("failed to delete", "host", h, "path", path, "error", err)
				}
				return nil
			}
			return nil
		})
		if err != nil {
			l.Warn(fmt.Sprintf("failed to walk %s", binary), "path", file, "error", err)
		}
	}
	return nil
}

// distroFilesFor is the package's own files as this host needs them uploaded, holding only the
// files a host running arch has any use for.
func (p *UploadFiles) distroFilesFor(ctx context.Context, h *cluster.Host, arch api.Arch) ([]v1alpha1.File, error) {
	files := []v1alpha1.File{}

	for i, f := range p.disFiles {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("upload canceled: %w", ctx.Err())
		}
		// A file's position in the list is the name of the directory it was assembled into, so a
		// file meant for another architecture is skipped here rather than filtered out of the list
		// beforehand. Filtering first would renumber every later file and read the wrong bytes.
		if !f.Selector.MatchesArch(arch) {
			logger.From(ctx).Debug("file is not for this host architecture", "file", filepath.Base(f.Target), "host", h, "arch", arch)
			continue
		}
		if f.Selector.Profile != "" && f.Selector.Profile != hostProfile(h) {
			logger.From(ctx).Debug("file is not for this host profile", "file", filepath.Base(f.Target), "host", h, "profile", f.Selector.Profile)
			continue
		}
		if !whenEnabled(ctx, p.manager.Values, f.Selector.When, filepath.Base(f.Target)) {
			continue
		}
		// No staging here. These are the package's own files rather than an install
		// method's, so no phase runs after this one to rename a staged file into place --
		// see the comment in profileFilesForArch. An executable goes straight to its target,
		// where uploadFile's permission handling already makes it executable.
		target := f.Target
		logger.From(ctx).Debug("need to upload from distro package", "source", filepath.Join(p.manager.TempDirectory, config.FilesDir, strconv.Itoa(i), filepath.Base(f.Target)), "target", target)
		files = append(files, v1alpha1.File{
			Name:           filepath.Base(f.Target),
			Target:         target,
			OriginalTarget: f.Target,
			TargetIsDir:    f.TargetIsDir,
			Category:       "file",
			LocalSource: v1alpha1.LocalFile{
				Path: filepath.Join(p.manager.TempDirectory, config.FilesDir, strconv.Itoa(i), filepath.Base(f.Target)),
			},
		})
	}

	return files, nil
}

func (p *UploadFiles) uploadDistroFiles(ctx context.Context, h *cluster.Host) error {
	arch, err := hostArch(h)
	if err != nil {
		return err
	}

	files, err := p.distroFilesFor(ctx, h, arch)
	if err != nil {
		return err
	}

	for _, f := range h.Files {
		if ctx.Err() != nil {
			return fmt.Errorf("upload canceled: %w", ctx.Err())
		}
		logger.From(ctx).Debug("need to upload", "target", f.Destination)
		if f.Data != "" {
			err := p.uploadData(ctx, h, &v1alpha1.File{
				Name:   filepath.Base(f.Destination),
				Target: f.Destination,
				Data:   f.Data,
			})
			if err != nil {
				logger.From(ctx).Warn("failed to upload data", "file", f.Destination)
			}
		}
	}

	for i, f := range files {
		logger.From(ctx).Debug("file", "num", i+1, "count", len(files))
		if err := p.uploadFile(ctx, h, &f); err != nil {
			logger.From(ctx).Warn("failed to upload", "file", f.Name, "host", h, "error", err)
		}
		if f.Executable {
			if err := h.Sudo().Exec(fmt.Sprintf("chmod +x %s", f.Target)); err != nil {
				logger.From(ctx).Warn("failed to add execute permission", "file", f, "host", h)
			}
		}
	}

	images := p.imgFiles[arch]
	for i, f := range images {
		logger.From(ctx).Debug("image", "num", i+1, "count", len(images), "arch", arch)
		if err := p.uploadFile(ctx, h, &f); err != nil {
			logger.From(ctx).Warn("failed to upload", "file", f.Name, "host", h, "error", err)
		}
	}

	return nil
}
