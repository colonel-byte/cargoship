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

// Package assemble builds a Cargoship package on disk
package assemble

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/colonel-byte/cargoship/api"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/contentstore"
	"github.com/colonel-byte/cargoship/pkg/engineconfig/extract"
	"github.com/colonel-byte/cargoship/pkg/engineconfig/gen"
	"github.com/colonel-byte/cargoship/pkg/fileoverride"
	"github.com/colonel-byte/cargoship/pkg/helmvalues"
	"github.com/colonel-byte/cargoship/pkg/helpers"
	"github.com/colonel-byte/cargoship/pkg/images"
	"github.com/colonel-byte/cargoship/pkg/packager/layout"
	"github.com/colonel-byte/cargoship/pkg/signing"
	"github.com/colonel-byte/cargoship/pkg/utils"
	goyaml "github.com/goccy/go-yaml"
	"github.com/k0sproject/dig"
	zlang "github.com/zarf-dev/zarf/src/config/lang"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/transform"
	"github.com/zarf-dev/zarf/src/pkg/value"
	"github.com/zarf-dev/zarf/src/types"
)

// AssembleOptions options
type AssembleOptions struct {
	RegistryOverrides []images.RegistryOverride
	// FileOverrides redirect the file downloads a distro definition declares to an internal
	// mirror or to a directory of pre-staged assets, the way RegistryOverrides redirects image
	// pulls. See pkg/fileoverride.
	FileOverrides []fileoverride.Override
	// ContentStore is an opened local content bundle that images and files are resolved from
	// before the network. It is nil when no store was given.
	ContentStore   ContentStore
	OCIConcurrency int
	CachePath      string
	SkipSBOM       bool
	// Reproducible pins Build.Timestamp to config.Timestamp instead of the
	// current time, and is recorded on Build.Reproducible, so identical package
	// inputs produce byte-identical output.
	Reproducible bool
	// SigningKeyPath and SigningKeyPassword sign the package as part of assembly when
	// set. Empty values are a no-op -- see DistroLayout.SignPackage.
	SigningKeyPath     string
	SigningKeyPassword string
	types.RemoteOptions
}

// keyDisable is the engine config key listing packaged components the engine must not deploy.
const keyDisable = "disable"

// distroTypeUpstream is Upstream's `.spec.type` id (distrocfg.DistroUpstream) -- repeated here
// rather than imported to avoid a dependency from this package onto src/types/distrocfg.
const distroTypeUpstream = "upstream"

// logUnknownEngineConfig logs engine config keys -- and `disable:` values -- the distro version
// being packaged does not recognize, so a typo is visible here rather than only on every node at
// install time.
//
// This only ever logs, at debug. The generated schemas cover the versions whose source has been
// pulled into this build, which is not necessarily the version a package targets, and a package
// that carries a key this binary has never heard of is still a package worth building. Install
// time keeps the authoritative check, where the key is narrowed to the role the node actually
// plays and an unrecognized one is dropped from the config it writes.
func logUnknownEngineConfig(ctx context.Context, d distro.Distro) {
	cfg := engineConfigKeys(d.Spec.Config.Engine[config.EngineConfig])
	if len(cfg) == 0 {
		return
	}

	l := logger.From(ctx)
	entry, ok := gen.Lookup(d.Spec.Type, d.Spec.Version)
	if !ok {
		l.Debug("no generated engine config schema for this distro/version, skipping config key validation",
			"distro", d.Spec.Type, "version", d.Spec.Version)
		return
	}

	if d.Spec.Type == distroTypeUpstream {
		if node, ok := entry.Server.(extract.FieldNode); ok {
			logUnknownNestedEngineConfig(ctx, d, node, cfg)
		}
		return
	}

	// A package installs controllers and workers alike, so a key either role accepts belongs
	// here.
	valid := gen.Keys(entry.Server)
	maps.Copy(valid, gen.Keys(entry.Agent))

	for _, k := range slices.Sorted(maps.Keys(cfg)) {
		if _, ok := valid[k]; !ok {
			l.Debug("engine config key not recognized for this distro/version, it will be dropped at install time",
				"distro", d.Spec.Type, "version", d.Spec.Version, "key", k)
		}
	}

	// Unlike an unrecognized key, an unrecognized packaged component survives install: the
	// addon vocabulary is composed rather than read off a flag list, so it is reported and
	// kept. See RancherCommon.warnUnknownAddons.
	for _, name := range gen.UnknownAddons(cfg[keyDisable], entry.Addons) {
		l.Debug("engine config disables a component this distro/version does not package",
			"distro", d.Spec.Type, "version", d.Spec.Version, "component", name)
	}
}

// logUnknownNestedEngineConfig is logUnknownEngineConfig's upstream twin: it recurses through
// cfg alongside node instead of checking a flat key set, since upstream's schema is a nested
// extract.FieldNode tree, not a flag-derived struct gen.Keys can reflect over. Warn-only, like
// its caller -- nothing here mutates cfg.
func logUnknownNestedEngineConfig(ctx context.Context, d distro.Distro, node extract.FieldNode, cfg map[string]any) {
	l := logger.From(ctx)
	for _, k := range slices.Sorted(maps.Keys(cfg)) {
		child, known := node.Children[k]
		if !known {
			l.Debug("engine config key not recognized for this distro/version, it will be dropped at install time",
				"distro", d.Spec.Type, "version", d.Spec.Version, "key", k)
			continue
		}
		if child.Children == nil {
			continue
		}
		if nested := engineConfigKeys(cfg[k]); len(nested) > 0 {
			logUnknownNestedEngineConfig(ctx, d, child, nested)
		}
	}
}

// engineConfigKeys is the engine config block, whichever map shape it was decoded into.
func engineConfigKeys(v any) map[string]any {
	switch m := v.(type) {
	case dig.Mapping:
		return m
	case map[string]any:
		return m
	default:
		return nil
	}
}

// AssembleDistro creates the actual tarballs
func AssembleDistro(ctx context.Context, d distro.Distro, distroPath string, opts AssembleOptions) (*layout.DistroLayout, error) {
	l := logger.From(ctx)
	l.Info("assembling distro", "path", distroPath)
	logUnknownEngineConfig(ctx, d)

	buildPath, err := utils.MakeTempDir(config.CommonOptions.TempDirectory)
	if err != nil {
		return nil, err
	}
	l.Debug("assembling distro in temp folder", "tmp", buildPath)

	// Values are resolved before anything that could reference them runs. The onCreate
	// actions below template against them, and the copies this writes into the build path
	// are covered by the checksums taken later. The order has one consequence worth
	// knowing: an onCreate action cannot write a values file that the same build reads.
	d, values, err := packageValues(ctx, d, distroPath, buildPath)
	if err != nil {
		return nil, err
	}

	onCreate := d.Spec.Actions.OnCreate
	// One variable config spans both action sets, so a variable the before actions
	// set is still readable by the after actions.
	varCfg := newVariableConfig(ctx)

	if err := runCreateActions(ctx, distroPath, onCreate.Defaults, onCreate.Before, values, varCfg); err != nil {
		return nil, fmt.Errorf("unable to run component before action: %w", err)
	}

	// A file that cannot be staged is fatal. It used to be logged and skipped, which meant a
	// checksum mismatch -- the one thing standing between a mirrored download and an arbitrary
	// binary -- cost a log line and shipped anyway.
	//
	// fileSources collects the files an override redirected, in staging order, which is
	// definition order. Appending in a fixed order is what keeps --reproducible reproducible.
	var fileSources []distro.FileSource

	// Built across both lists before anything is staged, so that an archive several entries
	// take members out of is fetched and decompressed once rather than once per entry.
	extractor := newArchiveExtractor(buildPath, d.Spec.Config.Files, d.Spec.Config.OS.Files)
	defer func() {
		err = errors.Join(err, extractor.cleanup())
	}()

	for filesIdx, file := range d.Spec.Config.Files {
		provenance, err := fileGrabber(ctx, string(config.FilesDir), buildPath, distroPath, filesIdx, *file, opts.FileOverrides, opts.ContentStore, opts.ociOptions(), extractor)
		if err != nil {
			return nil, fmt.Errorf("unable to stage file %d (%s): %w", filesIdx, file, err)
		}
		if provenance != nil {
			fileSources = append(fileSources, *provenance)
		}
	}
	for filesIdx, file := range d.Spec.Config.OS.Files {
		provenance, err := fileGrabber(ctx, string(config.OSDir), buildPath, distroPath, filesIdx, *file, opts.FileOverrides, opts.ContentStore, opts.ociOptions(), extractor)
		if err != nil {
			return nil, fmt.Errorf("unable to stage os file %d (%s): %w", filesIdx, file, err)
		}
		if provenance != nil {
			fileSources = append(fileSources, *provenance)
		}
	}

	componentImages := []transform.Image{}
	for _, src := range d.Spec.Config.AllImages() {
		refInfo, err := transform.ParseImageRef(src)
		if err != nil {
			return nil, fmt.Errorf("failed to create ref for image %s: %w", src, err)
		}
		if slices.Contains(componentImages, refInfo) {
			continue
		}
		componentImages = append(componentImages, refInfo)
	}

	if len(componentImages) > 0 {
		arches := d.Metadata.Arches()
		pullOpts := images.PullOptions{
			Source:                opts.ContentStore,
			OCIConcurrency:        opts.OCIConcurrency,
			Arches:                archStrings(arches),
			RegistryOverrides:     opts.RegistryOverrides,
			CacheDirectory:        filepath.Join(opts.CachePath, config.ImagesDir),
			PlainHTTP:             opts.PlainHTTP,
			InsecureSkipTLSVerify: opts.InsecureSkipTLSVerify,
		}
		imagesPath := filepath.Join(buildPath, config.ImagesDir)
		l.Info("pulling images too", "path", imagesPath, "architectures", api.FormatArches(arches))
		_, err := images.Pull(ctx, componentImages, imagesPath, pullOpts)
		if err != nil {
			return nil, err
		}
	}

	if err := runCreateActions(ctx, distroPath, onCreate.Defaults, onCreate.After, values, varCfg); err != nil {
		return nil, fmt.Errorf("unable to run component after action: %w", err)
	}

	checksumContent, checksumSha, err := getChecksum(buildPath)
	if err != nil {
		return nil, err
	}
	checksumPath := filepath.Join(buildPath, config.Checksums)
	err = os.WriteFile(checksumPath, []byte(checksumContent), helpers.ReadAllWriteUser)
	if err != nil {
		return nil, err
	}
	d.Metadata.AggregateChecksum = checksumSha

	d = recordDistroMetadata(d, opts, fileSources)

	b, err := goyaml.Marshal(d)
	if err != nil {
		return nil, err
	}
	err = os.WriteFile(filepath.Join(buildPath, config.DistroYAML), b, helpers.ReadAllWriteUser)
	if err != nil {
		return nil, err
	}

	distroLayout := layout.NewDistroLayout(buildPath, d)

	signOpts := signing.DefaultSignBlobOptions()
	signOpts.Key = opts.SigningKeyPath
	signOpts.Password = opts.SigningKeyPassword
	if err := distroLayout.SignPackage(ctx, layout.SignOptions{SignBlobOptions: signOpts}); err != nil {
		return nil, err
	}

	return distroLayout, nil
}

// packageValues copies the distro's values files and their schema into the
// package, rewrites the spec to point at those copies, validates the merged
// values against the schema, and returns the merged values.
//
// Values are resolved once, here, and the resolved copies are what both the
// validation and the package see. A values file referenced by URL is therefore
// fetched at create time and shipped: an install into an air-gapped cluster
// must not depend on reaching that URL again, and a package that validated at
// create time must not become invalid later because the URL changed.
//
// The returned values are what the onCreate actions template against. They are the
// package's own defaults: a cluster's overrides do not exist yet at create time, so an
// action sees the same values every build, while the surfaces rendered at install time see
// the overridden ones.
func packageValues(ctx context.Context, d distro.Distro, distroPath string, buildPath string) (_ distro.Distro, _ value.Values, err error) {
	vals := d.Spec.Values
	if len(vals.Files) == 0 && vals.Schema == "" {
		return d, nil, nil
	}
	l := logger.From(ctx)

	valuesPath := filepath.Join(buildPath, config.ValuesDir)
	if err := os.MkdirAll(valuesPath, helpers.ReadExecuteAllWriteUser); err != nil {
		return d, nil, err
	}

	tmpDir, err := utils.MakeTempDir(config.CommonOptions.TempDirectory)
	if err != nil {
		return d, nil, err
	}
	defer func() {
		err = errors.Join(err, os.RemoveAll(tmpDir))
	}()

	sources, err := helmvalues.ResolveFiles(ctx, distroPath, tmpDir, vals.Files)
	if err != nil {
		return d, nil, fmt.Errorf("unable to resolve values files: %w", err)
	}

	// The index prefix keeps merge order visible in the package and keeps two
	// values files that share a base name from overwriting each other.
	packaged := make([]string, 0, len(sources))
	for i, src := range sources {
		rel := filepath.Join(config.ValuesDir, fmt.Sprintf("%d-%s", i, filepath.Base(src)))
		if err := helpers.CreatePathAndCopy(src, filepath.Join(buildPath, rel)); err != nil {
			return d, nil, fmt.Errorf("unable to add values file %s to the package: %w", vals.Files[i], err)
		}
		packaged = append(packaged, filepath.ToSlash(rel))
	}

	if vals.Schema != "" {
		src := vals.Schema
		if !filepath.IsAbs(src) {
			src = filepath.Join(distroPath, src)
		}
		rel := filepath.Join(config.ValuesDir, config.ValuesSchema)
		if err := helpers.CreatePathAndCopy(src, filepath.Join(buildPath, rel)); err != nil {
			return d, nil, fmt.Errorf("unable to add values schema %s to the package: %w", vals.Schema, err)
		}
		vals.Schema = filepath.ToSlash(rel)
	}
	vals.Files = packaged
	d.Spec.Values = vals

	// Validate what shipped, not what was pointed at.
	merged, err := layout.LoadValues(ctx, buildPath, d.Spec.Values)
	if err != nil {
		return d, nil, err
	}
	l.Debug("packaged values", "files", len(packaged), "keys", len(merged))

	checkMappings(ctx, d, merged)

	return d, merged, nil
}

// checkMappings applies the distro's value mappings to a throwaway copy of the engine
// config and reports the ones that cannot be applied: a path missing its leading dot, or a
// target under a chart entry that is a string rather than a map.
//
// Without this, the first time anyone finds out is on the cluster being installed, because
// install time is the only thing that applies a mapping for real. It warns rather than
// fails: a mapping that has always been wrong should not turn a package that builds today
// into one that does not.
func checkMappings(ctx context.Context, d distro.Distro, values map[string]any) {
	if len(d.Spec.Values.Mappings) == 0 {
		return
	}

	l := logger.From(ctx)
	engine := d.Spec.Config.Engine.Dup()
	for _, m := range d.Spec.Values.Mappings {
		if _, err := helmvalues.ApplyMapping(engine, values, m.Source, m.Target); err != nil {
			l.Warn("values mapping cannot be applied and will fail at install time",
				"source", m.Source, "target", m.Target, "error", err)
		}
	}
}

// fileArch returns the one architecture a file entry selects, or "" when it selects none or
// several.
//
// A file is staged arch-blind -- fileGrabber keys its output on the entry's index in the list,
// and selector.arch is not read again until install time -- so this is the only thing that
// tells a build which platform of a multi-platform source an entry meant. A resolver that
// needs a platform and gets "" refuses rather than guessing; see issue #1033.
func fileArch(file v1alpha1.File) string {
	if len(file.Selector.Arch) != 1 {
		return ""
	}
	return string(file.Selector.Arch[0])
}

// fileGrabber stages one file into buildPath. The returned FileSource is non-nil only when the
// bytes came from somewhere other than the declared source -- an override, or a content store --
// so the caller records what actually happened rather than only what was configured.
func fileGrabber(ctx context.Context, resourceType string, buildPath string, distroPath string, filesIdx int, file v1alpha1.File, overrides []fileoverride.Override, store ContentStore, ociOpts ociOptions, extractor *archiveExtractor) (provenance *distro.FileSource, err error) {
	if extractor == nil {
		// A caller staging one file on its own -- which is every test, and nothing in a real
		// build -- gets an extractor that plans for just this entry.
		extractor = newArchiveExtractor(filepath.Dir(buildPath), []*v1alpha1.File{&file})
		defer func() {
			err = errors.Join(err, extractor.cleanup())
		}()
	}

	rel := filepath.Join(resourceType, strconv.Itoa(filesIdx), filepath.Base(file.Target))
	dst := filepath.Join(buildPath, rel)

	// Leave nothing half-written behind. buildPath is walked later to generate the package
	// checksums, so a file left by a failed download, a failed extraction, or a mismatched
	// shasum would otherwise be picked up and shipped.
	defer func() {
		if err != nil {
			err = errors.Join(err, os.RemoveAll(dst))
		}
	}()

	// Resolve any override before the branch below, because an override can turn a URL source
	// into a local path and so change which half of this function runs. file.Source itself is
	// left alone: it is what the definition declared, and it is what the recorded provenance
	// and the "this came from upstream" reading of the package refer to.
	source := file.Source
	match, overridden, err := fileoverride.Resolve(overrides, file.Source)
	if err != nil {
		return nil, err
	}
	if overridden {
		// An override points the build at bytes whoever wrote the distro definition never
		// saw. The declared shasum is the only thing that makes that safe, so a file without
		// one is refused rather than fetched unverified from somebody's mirror.
		if file.Shasum == "" {
			return nil, fmt.Errorf("file override %s=%s applies to %s, which declares no shasum: refusing to fetch it unverified",
				match.Override.Source, match.Override.Target, file.Source)
		}
		logger.From(ctx).Info("file source overridden",
			"declared", file.Source, "resolved", match.Resolved, "override", match.Override.Source)
		source = match.Resolved
		provenance = &distro.FileSource{
			Path:     filepath.ToSlash(rel),
			Declared: file.Source,
			Resolved: match.Resolved,
			Override: match.Override.Source,
			Shasum:   file.Shasum,
		}
	}

	// A content store is consulted only when no override matched: an operator who named a
	// redirect for this file asked for that specific one, and the store should not quietly win
	// over it. What the store yields is a local file, so the staging below hands off to the
	// local-source branch and reuses its extraction, verification and permissions unchanged.
	if !overridden && store != nil {
		req := contentstore.FileRequest{Source: file.Source, Arch: fileArch(file), Layer: file.Layer}
		name, held, err := store.File(req)
		if err != nil {
			return nil, err
		}
		switch {
		case held:
			// Store content is bytes whoever wrote the distro definition never saw, exactly
			// like override content, so the same rule applies: without a declared shasum
			// there is nothing to verify it against and it is refused.
			if file.Shasum == "" {
				return nil, fmt.Errorf("%s holds %s, which declares no shasum: refusing to stage it unverified",
					store.Describe(), file.Source)
			}
			staged, err := os.MkdirTemp("", "cargoship-store-file-")
			if err != nil {
				return nil, fmt.Errorf("unable to create a staging directory for %s: %w", file.Source, err)
			}
			defer func() {
				err = errors.Join(err, os.RemoveAll(staged))
			}()
			source = filepath.Join(staged, name)
			if err := store.Fetch(ctx, req, source); err != nil {
				return nil, err
			}
			logger.From(ctx).Info("file read from content store",
				"declared", file.Source, "name", name, "store", store.Source())
			provenance = &distro.FileSource{
				Path:     filepath.ToSlash(rel),
				Declared: file.Source,
				Resolved: name,
				Store:    store.Source(),
				Shasum:   file.Shasum,
			}
		case helpers.IsURL(file.Source):
			// Only a remote source is worth reporting as absent: a definition's local paths
			// are read from the definition directory and were never the store's to hold.
			logger.From(ctx).Warn("file not held by the content store, falling back to its declared source",
				"declared", file.Source, "store", store.Source())
		}
	}

	// A registry override applies to an oci:// source for the same reason it applies to an
	// image pull: an operator who redirects a registry means it for everything cargoship
	// fetches from there. It is resolved here rather than in the fetch so that the refusal
	// and the provenance record sit beside the ones --file-override writes.
	//
	// Note that --file-override itself never matches an oci:// source: pkg/fileoverride
	// parses http and https only.
	if helpers.IsOCIURL(source) && provenance == nil {
		// Matched on the reference without its scheme, because an override's source is a
		// registry prefix as an image list spells one -- "registry.example.com", with no
		// oci:// in front of it.
		rewritten, match, ok := images.ApplyOverride(strings.TrimPrefix(source, helpers.OCIURLPrefix), ociOpts.RegistryOverrides)
		if ok {
			rewritten = helpers.OCIURLPrefix + rewritten
			// Overridden bytes come from a registry the definition's author never named,
			// so something has to tie them to what upstream published. A digest in the
			// reference does that by itself; otherwise the declared shasum is the only
			// thing left, and without it the override is an unauthenticated substitution.
			if file.Shasum == "" && !pinnedByDigest(source) {
				return nil, fmt.Errorf("registry override %s=%s applies to %s, which declares no shasum and is not pinned by digest: refusing to fetch it unverified",
					match.Source, match.Override, file.Source)
			}
			logger.From(ctx).Info("file source overridden",
				"declared", file.Source, "resolved", rewritten, "override", match.Source)
			source = rewritten
			provenance = &distro.FileSource{
				Path:     filepath.ToSlash(rel),
				Declared: file.Source,
				Resolved: rewritten,
				Override: match.Source,
				Shasum:   file.Shasum,
			}
		}
	}

	// An entry with an extractPath takes one member out of an archive that several entries
	// usually share, so the archive is fetched and walked once by the extractor and every
	// member is copied out of what that produced. An entry without one is staged straight to
	// dst, which is what the three arms below differ on.
	if file.ExtractPath != "" {
		member, err := extractor.member(ctx, keyFor(file), file.ExtractPath,
			newArchiveSource(source, file, distroPath, ociOpts))
		if err != nil {
			return nil, err
		}
		// Copied rather than renamed, because a second entry wants the same member staged
		// under its own target name. Copying into dst also means the extraction's own
		// directories never occupy dst, which renaming into place used to collide with: an
		// extractPath whose leading segment matches the target's base name -- zarf's
		// "zarf-injector/files/0/zarf-injector" for a target named zarf-injector -- produced
		// a "file exists" naming a path the definition never mentions.
		if err := helpers.CreatePathAndCopy(member, dst); err != nil {
			return nil, fmt.Errorf(zlang.ErrWritingFile, dst, err)
		}
	} else if helpers.IsOCIURL(source) {
		if err := fetchOCIFile(ctx, source, file, fileArch(file), dst, ociOpts); err != nil {
			return nil, err
		}
	} else if helpers.IsURL(source) {
		if err := utils.DownloadToFileWithChecksum(ctx, source, dst, file.Shasum, filepath.Base(file.Target)); err != nil {
			return nil, fmt.Errorf(zlang.ErrDownloading, source, err)
		}
	} else {
		if err := helpers.CreatePathAndCopy(localSource(source, distroPath), dst); err != nil {
			return nil, fmt.Errorf("unable to copy file %s: %w", source, err)
		}
	}

	// Abort packaging on invalid shasum (if one is specified).
	if file.Shasum != "" {
		if err := helpers.SHAsMatch(dst, file.Shasum); err != nil {
			return nil, fmt.Errorf("sha mismatch for %s: %w", source, err)
		}
	}

	if file.Executable || helpers.IsDir(dst) {
		err := os.Chmod(dst, helpers.ReadExecuteAllWriteUser)
		if err != nil {
			return nil, err
		}
	} else {
		err := os.Chmod(dst, helpers.ReadAllWriteUser)
		if err != nil {
			return nil, err
		}
	}
	return provenance, nil
}

// buildTimestamp returns the timestamp to record as Build.Timestamp. When
// reproducible is true (--reproducible), it's pinned to config.Timestamp instead
// of the current time, so two builds from identical inputs produce byte-identical
// output. Mirrors "flux push artifact --reproducible".
func buildTimestamp(reproducible bool) time.Time {
	if reproducible {
		return config.Timestamp
	}
	return time.Now()
}

// archStrings converts the architectures a package targets into the plain strings the image puller
// takes. Every architecture is pulled into the one images directory, where an image that resolves to
// more than one manifest is stored as an index.
func archStrings(arches api.Arches) []string {
	out := make([]string, 0, len(arches))
	for _, arch := range arches {
		out = append(out, string(arch))
	}
	return out
}

func recordDistroMetadata(d distro.Distro, opts AssembleOptions, fileSources []distro.FileSource) distro.Distro {
	arches := d.Metadata.Arches()
	d.Build.Architectures = arches
	// The scalar stays populated for a single architecture package so that readers which only know
	// about it, such as an older cargoship, still see the architecture they expect.
	if len(arches) == 1 {
		d.Build.Architecture = arches[0]
	}
	d.Build.Timestamp = buildTimestamp(opts.Reproducible).Format(api.BuildTimestampFormat)
	d.Build.Version = d.Metadata.Version
	d.Build.Reproducible = opts.Reproducible

	overrides := make(map[string]string, len(opts.RegistryOverrides))
	for i := range opts.RegistryOverrides {
		overrides[opts.RegistryOverrides[i].Source] = opts.RegistryOverrides[i].Override
	}

	d.Build.RegistryOverrides = overrides
	d.Build.FileOverrides = fileoverride.ToMap(opts.FileOverrides)
	d.Build.FileSources = fileSources

	return d
}

func getChecksum(dirPath string) (string, string, error) {
	checksumData := []string{}
	err := filepath.Walk(dirPath, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dirPath, path)
		if err != nil {
			return err
		}
		if rel == config.DistroYAML || rel == config.Checksums {
			return nil
		}
		sum, err := helpers.GetSHA256OfFile(path)
		if err != nil {
			return err
		}
		checksumData = append(checksumData, fmt.Sprintf("%s %s", sum, filepath.ToSlash(rel)))
		return nil
	})
	if err != nil {
		return "", "", err
	}
	slices.Sort(checksumData)

	checksumContent := strings.Join(checksumData, "\n") + "\n"
	sha := sha256.Sum256([]byte(checksumContent))
	return checksumContent, hex.EncodeToString(sha[:]), nil
}

// ContentStore is a local, already-populated bundle of images and files that a build resolves
// against before reaching the network: a hauler store, a zarf package.
//
// The image half is images.ImageSource unchanged, so pkg/images needs to know nothing about any
// particular bundle. The file half is declared here, at the consumer, and deliberately hands
// back a name and bytes rather than an OCI descriptor -- a hauler store holds a file as a
// titled layer, a zarf package holds it as a member of a component tarball, and only one of
// those has a descriptor to give.
//
// A reader must never be placed in this field as a typed nil pointer, which is not itself nil
// and would send every lookup to a bundle that is not there. See openContentStore in pkg/distro.
type ContentStore interface {
	images.ImageSource
	// File reports whether the bundle holds the file the definition's entry asks for, and
	// the name it is restored under. It errors when the bundle holds the reference for
	// several architectures and the request names none -- see contentstore.FileRequest.
	File(req contentstore.FileRequest) (name string, ok bool, err error)
	// Fetch writes that file to dst.
	Fetch(ctx context.Context, req contentstore.FileRequest, dst string) error
	// Source names the bundle as the operator spelled it, for messages and provenance.
	Source() string
}
