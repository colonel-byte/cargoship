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
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/pkg/engineconfig/extract"
	"github.com/colonel-byte/cargoship/pkg/engineconfig/gen"
	"github.com/colonel-byte/cargoship/pkg/fileoverride"
	"github.com/colonel-byte/cargoship/pkg/helmvalues"
	"github.com/colonel-byte/cargoship/pkg/helpers"
	"github.com/colonel-byte/cargoship/pkg/images"
	"github.com/colonel-byte/cargoship/pkg/packager/layout"
	"github.com/colonel-byte/cargoship/pkg/utils"
	goyaml "github.com/goccy/go-yaml"
	"github.com/k0sproject/dig"
	zlang "github.com/zarf-dev/zarf/src/config/lang"
	"github.com/zarf-dev/zarf/src/pkg/archive"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/signing"
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
	FileOverrides  []fileoverride.Override
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
func logUnknownEngineConfig(ctx context.Context, d distro.ZarfDistro) {
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
func logUnknownNestedEngineConfig(ctx context.Context, d distro.ZarfDistro, node extract.FieldNode, cfg map[string]any) {
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
func AssembleDistro(ctx context.Context, d distro.ZarfDistro, distroPath string, opts AssembleOptions) (*layout.DistroLayout, error) {
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

	// One archive commonly supplies several files -- every rke2 binary, script and unit file
	// is extracted from the same tarball -- so archives are downloaded once into a cache that
	// spans both file loops. A cached archive has to outlive the fileGrabber call that fetched
	// it, so the cache is owned here and torn down once every file is staged.
	archives := newDownloadCache()
	defer func() {
		if err := archives.cleanup(); err != nil {
			l.Warn("unable to remove downloaded archive cache", "error", err)
		}
	}()

	for filesIdx, file := range d.Spec.Config.Files {
		provenance, err := fileGrabber(ctx, string(config.FilesDir), buildPath, distroPath, filesIdx, *file, opts.FileOverrides, archives)
		if err != nil {
			return nil, fmt.Errorf("unable to stage file %d (%s): %w", filesIdx, file, err)
		}
		if provenance != nil {
			fileSources = append(fileSources, *provenance)
		}
	}
	for filesIdx, file := range d.Spec.Config.OS.Files {
		provenance, err := fileGrabber(ctx, string(config.OSDir), buildPath, distroPath, filesIdx, *file, opts.FileOverrides, archives)
		if err != nil {
			return nil, fmt.Errorf("unable to stage os file %d (%s): %w", filesIdx, file, err)
		}
		if provenance != nil {
			fileSources = append(fileSources, *provenance)
		}
	}

	componentImages := []transform.Image{}
	for _, src := range d.Spec.Config.ImagesConfig.Images {
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
	if err := distroLayout.SignPackage(ctx, signOpts); err != nil {
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
func packageValues(ctx context.Context, d distro.ZarfDistro, distroPath string, buildPath string) (_ distro.ZarfDistro, _ value.Values, err error) {
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
func checkMappings(ctx context.Context, d distro.ZarfDistro, values map[string]any) {
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

// downloadCache remembers the archives already downloaded during one assemble, so several
// files extracted from the same URL cost one download rather than one each.
//
// The cache owns a directory of its own, created on first use and removed by cleanup. That is
// what distinguishes it from a plain map: a cached archive is read again by a later
// fileGrabber call, so it cannot be cleaned up by the call that downloaded it.
//
// A nil *downloadCache is a working cache that never hits, for callers staging a single file.
type downloadCache struct {
	// dir holds the downloaded archives, empty until the first one is reserved.
	dir string
	// paths maps a resolved source URL to the archive downloaded from it.
	paths map[string]string
}

// newDownloadCache returns an empty cache. No directory is created until something is cached,
// so an assemble that stages no remote archives leaves nothing behind.
func newDownloadCache() *downloadCache {
	return &downloadCache{paths: map[string]string{}}
}

// get returns the archive already downloaded from url, if there is one.
func (c *downloadCache) get(url string) (string, bool) {
	if c == nil {
		return "", false
	}
	path, ok := c.paths[url]
	return path, ok
}

// reserve names the path an archive called name should be downloaded to, creating the cache
// directory on first use. Each archive gets its own numbered subdirectory, so two archives
// whose URLs end in the same file name do not overwrite each other.
func (c *downloadCache) reserve(name string) (string, error) {
	if c == nil {
		dir, err := utils.MakeTempDir(config.CommonOptions.TempDirectory)
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, name), nil
	}
	if c.dir == "" {
		dir, err := utils.MakeTempDir(config.CommonOptions.TempDirectory)
		if err != nil {
			return "", err
		}
		c.dir = dir
	}
	dir := filepath.Join(c.dir, strconv.Itoa(len(c.paths)))
	if err := os.MkdirAll(dir, helpers.ReadWriteExecuteUser); err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// put records that url's archive was downloaded to path, so the next file extracted from url
// reuses it. A nil cache drops the record, which is what makes its get always miss.
func (c *downloadCache) put(url, path string) {
	if c != nil {
		c.paths[url] = path
	}
}

// cleanup removes every archive the cache downloaded. Safe to call on a cache that never
// downloaded anything, and on a nil cache.
func (c *downloadCache) cleanup() error {
	if c == nil || c.dir == "" {
		return nil
	}
	err := os.RemoveAll(c.dir)
	c.dir = ""
	return err
}

// fileGrabber stages one file into buildPath. The returned FileSource is non-nil only when an
// override redirected the file, so the caller records what an override actually did rather than
// only what was configured.
//
// archives may be nil, in which case any archive downloaded here is removed before returning.
// A caller staging more than one file passes a shared cache so that an archive several files
// are extracted from is downloaded once; see newDownloadCache.
func fileGrabber(ctx context.Context, resourceType string, buildPath string, distroPath string, filesIdx int, file v1alpha1.ZarfFile, overrides []fileoverride.Override, archives *downloadCache) (provenance *distro.FileSource, err error) {
	rel := filepath.Join(resourceType, strconv.Itoa(filesIdx), filepath.Base(file.Target))
	dst := filepath.Join(buildPath, rel)
	destinationDir := filepath.Dir(dst)

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

	if helpers.IsURL(source) {
		if file.ExtractPath != "" {
			// The temp name is local bookkeeping, so it comes from the declared source: a
			// mirror is free to serve the same artifact under a different path, and the
			// name the definition used is the more recognizable of the two in a log line.
			compressedFileName, err := helpers.ExtractBasePathFromURL(file.Source)
			if err != nil {
				return nil, fmt.Errorf(zlang.ErrFileNameExtract, file.Source, err)
			}

			compressedFile, cached := archives.get(source)
			if !cached {
				compressedFile, err = archives.reserve(compressedFileName)
				if err != nil {
					return nil, err
				}
				// An archive this call downloaded is only reachable through the cache once the
				// download has succeeded, so an uncached one -- including a failed download --
				// is removed here rather than left in the temp directory.
				defer func() {
					if _, ok := archives.get(source); !ok {
						err = errors.Join(err, os.RemoveAll(filepath.Dir(compressedFile)))
					}
				}()

				// file.Shasum is deliberately not passed here. With ExtractPath set it is the
				// digest of the extracted file, which the check at the end of this function
				// makes against dst -- the archive it came out of has a different one.
				if err := utils.DownloadToFileWithChecksum(ctx, source, compressedFile, "", filepath.Base(file.Target)); err != nil {
					return nil, fmt.Errorf(zlang.ErrDownloading, source, err)
				}
				archives.put(source, compressedFile)
			}

			decompressOpts := archive.DecompressOpts{
				Files: []string{file.ExtractPath},
			}
			err = archive.Decompress(ctx, compressedFile, destinationDir, decompressOpts)
			if err != nil {
				return nil, fmt.Errorf(zlang.ErrFileExtract, file.ExtractPath, compressedFileName, err)
			}
		} else {
			if err := utils.DownloadToFileWithChecksum(ctx, source, dst, file.Shasum, filepath.Base(file.Target)); err != nil {
				return nil, fmt.Errorf(zlang.ErrDownloading, source, err)
			}
		}
	} else {
		// A resolved local override is already absolute, so it is never re-resolved against
		// the distro definition directory here.
		src := source
		if !filepath.IsAbs(source) {
			src = filepath.Join(distroPath, source)
		}
		if file.ExtractPath != "" {
			decompressOpts := archive.DecompressOpts{
				Files: []string{file.ExtractPath},
			}
			err := archive.Decompress(ctx, src, destinationDir, decompressOpts)
			if err != nil {
				return nil, fmt.Errorf(zlang.ErrFileExtract, file.ExtractPath, src, err)
			}
		} else {
			if err := helpers.CreatePathAndCopy(src, dst); err != nil {
				return nil, fmt.Errorf("unable to copy file %s: %w", src, err)
			}
		}
	}

	if file.ExtractPath != "" {
		// Make sure dst reflects the actual file or directory.
		updatedExtractedFileOrDir := filepath.Join(destinationDir, file.ExtractPath)
		if updatedExtractedFileOrDir != dst {
			if err := os.Rename(updatedExtractedFileOrDir, dst); err != nil {
				return nil, fmt.Errorf(zlang.ErrWritingFile, dst, err)
			}
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

func recordDistroMetadata(d distro.ZarfDistro, opts AssembleOptions, fileSources []distro.FileSource) distro.ZarfDistro {
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
