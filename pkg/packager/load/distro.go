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

package load

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/colonel-byte/cargoship/api"
	v1alpha1 "github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/internal/cfg"
	"github.com/colonel-byte/cargoship/pkg/packager/layout"
	"github.com/colonel-byte/cargoship/pkg/schema"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/types"
)

// DefinitionOptions are the optional parameters to load.PackageDefinition
type DefinitionOptions struct {
	CachePath string
	// Architecture narrows a package that targets several architectures down to a single one.
	// A package that targets one architecture ignores it: the definition already names the only
	// architecture it carries files for.
	Architecture string
	types.RemoteOptions
}

// DistroDefinition returns a ZarfDistro object
func DistroDefinition(ctx context.Context, distroPath string, opts DefinitionOptions) (v1alpha1.Distro, error) {
	l := logger.From(ctx)
	start := time.Now()
	l.Debug("start layout.DistroDefinition", "path", distroPath)

	disPath, err := layout.ResolveDistroPath(distroPath)
	if err != nil {
		return v1alpha1.Distro{}, err
	}

	b, err := os.ReadFile(disPath.ManifestFile)
	if err != nil {
		return v1alpha1.Distro{}, err
	}
	dis, err := cfg.Parse(ctx, b)
	if err != nil {
		return v1alpha1.Distro{}, err
	}
	// Checked after the parse so that a syntax error is reported by the parser, which says
	// where, and before anything is resolved so that a definition is never half-processed on
	// the way to being refused.
	if err := validateAgainstSchema(disPath.ManifestFile, b); err != nil {
		return v1alpha1.Distro{}, err
	}
	dis, declared, err := resolveArchitectures(dis, opts.Architecture)
	if err != nil {
		return v1alpha1.Distro{}, err
	}

	err = validateDistro(ctx, dis, declared, disPath.ManifestFile)
	if err != nil {
		return v1alpha1.Distro{}, err
	}
	l.Debug("done layout.DistroDefinition", "duration", time.Since(start))
	return dis, nil
}

// validateAgainstSchema checks a definition against the published package schema.
//
// This is the same check `cargoship validate` runs, applied where a package is built. Until it
// was, the two disagreed about what a valid definition is: every constraint the schema states --
// the patterns, the enums, the lengths, and the 25 `additionalProperties: false` blocks that make
// a misspelled key an error -- was enforced by an editor and by an explicit `cargoship validate`,
// and by nothing at all on the path that actually produces a package. A key nobody validated was
// silently dropped by the decoder, so a definition with `imagePullPolciy` built a package missing
// whatever that key was meant to set, and said nothing.
//
// It validates the bytes rather than the decoded struct, which is the whole point. The decoder
// has already discarded an unknown key by the time a struct exists, so validating the struct
// cannot see the typo that matters most.
func validateAgainstSchema(path string, b []byte) error {
	doc, err := schema.Decode(b)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	validator, err := schema.NewValidatorFor(schema.KindPackage)
	if err != nil {
		return err
	}
	return validator.Validate(path, doc)
}

// resolveArchitectures settles which architectures the package targets.
//
// A definition that names no architecture at all takes the requested one, falling back to the
// architecture of the machine running cargoship. A definition that does name architectures is taken
// at its word: a request narrows the architectures list to a single entry, and a request that the
// package does not target is an error rather than an override, because the file entries for that
// architecture were never packaged.
//
// It returns the narrowed package alongside the architectures the definition declared, which is
// what the file selectors have to be validated against: a definition narrowed to amd64 still
// carries the arm64 entries it was written with, and those entries are not a mistake.
func resolveArchitectures(dis v1alpha1.Distro, requested string) (v1alpha1.Distro, api.Arches, error) {
	var arch api.Arch
	if requested != "" {
		parsed, err := api.ParseArch(requested)
		if err != nil {
			return dis, dis.Metadata.Arches(), err
		}
		arch = parsed
	}

	if len(dis.Metadata.Architectures) == 0 {
		if dis.Metadata.Architecture == "" {
			dis.Metadata.Architecture = api.Arch(config.GetArch(string(arch)))
			return dis, dis.Metadata.Arches(), nil
		}
		if arch != "" && arch != dis.Metadata.Architecture {
			return dis, dis.Metadata.Arches(), fmt.Errorf("architecture %q is not targeted by this package, which targets %s",
				arch, dis.Metadata.Architecture)
		}

		return dis, dis.Metadata.Arches(), nil
	}

	declared := dis.Metadata.Architectures

	if arch == "" {
		return dis, declared, nil
	}

	if !slices.Contains(declared, arch) {
		return dis, declared, fmt.Errorf("architecture %q is not targeted by this package, which targets %s",
			arch, api.FormatArches(declared))
	}

	dis.Metadata.Architectures = api.Arches{arch}

	return dis, declared, nil
}

func validateDistro(_ context.Context, dis v1alpha1.Distro, declared api.Arches, path string) error {
	// Reject an image compression format cargoship cannot write here, so a typo
	// fails at build time instead of halfway through an apply.
	if _, err := dis.Spec.Config.ImagesConfig.TarballSuffix(); err != nil {
		return err
	}

	if err := validateArchitectures(dis, declared); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	// The schema bounds these too, but nothing on this path applies the schema -- only
	// `cargoship validate` and an editor do -- so without this a package builds happily and
	// fails at the registry on the far side of the gap.
	if err := v1alpha1.ValidateReferenceLengths(dis.Metadata.Name, dis.Metadata.Version); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	return nil
}

// validateArchitectures checks that the package targets a sane set of architectures, and that no
// file selects an architecture the definition never declared. The second check turns a typo such as
// x86_64 into a build time error rather than a file that silently never uploads. It is checked
// against the declared architectures rather than what the package was narrowed to, so that
// narrowing a multi-architecture definition to one architecture is not itself an error.
func validateArchitectures(dis v1alpha1.Distro, declared api.Arches) error {
	arches := declared
	if len(arches) == 0 {
		return errors.New("package must target at least one architecture")
	}

	seen := make(map[api.Arch]struct{}, len(arches))
	for _, arch := range arches {
		if err := arch.Validate(); err != nil {
			return fmt.Errorf("architecture %w", err)
		}
		if _, ok := seen[arch]; ok {
			return fmt.Errorf("architecture %q is listed more than once", arch)
		}
		seen[arch] = struct{}{}
	}

	for _, f := range slices.Concat(dis.Spec.Config.Files, dis.Spec.Config.OS.Files) {
		if f == nil {
			continue
		}
		for _, arch := range f.Selector.Arch {
			if !slices.Contains(arches, arch) {
				return fmt.Errorf("file %q selects architecture %q, but the package declares %s",
					f.Target, arch, api.FormatArches(arches))
			}
		}
	}

	return nil
}
