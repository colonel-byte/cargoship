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

	"github.com/colonel-byte/cargoship/src/api"
	v1alpha1 "github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/internal/cfg"
	"github.com/colonel-byte/cargoship/src/pkg/packager/layout"
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
func DistroDefinition(ctx context.Context, distroPath string, opts DefinitionOptions) (v1alpha1.ZarfDistro, error) {
	l := logger.From(ctx)
	start := time.Now()
	l.Debug("start layout.DistroDefinition", "path", distroPath)

	disPath, err := layout.ResolveDistroPath(distroPath)
	if err != nil {
		return v1alpha1.ZarfDistro{}, err
	}

	b, err := os.ReadFile(disPath.ManifestFile)
	if err != nil {
		return v1alpha1.ZarfDistro{}, err
	}
	dis, err := cfg.Parse(ctx, b)
	if err != nil {
		return v1alpha1.ZarfDistro{}, err
	}
	dis, err = resolveArchitectures(dis, opts.Architecture)
	if err != nil {
		return v1alpha1.ZarfDistro{}, err
	}

	err = validateDistro(ctx, dis, disPath.ManifestFile)
	if err != nil {
		return v1alpha1.ZarfDistro{}, err
	}
	l.Debug("done layout.DistroDefinition", "duration", time.Since(start))
	return dis, nil
}

// resolveArchitectures settles which architectures the package targets.
//
// A definition that names no architecture at all takes the requested one, falling back to the
// architecture of the machine running cargoship. A definition that does name architectures is taken
// at its word: a request narrows the architectures list to a single entry, and a request that the
// package does not target is an error rather than an override, because the file entries for that
// architecture were never packaged.
func resolveArchitectures(dis v1alpha1.ZarfDistro, requested string) (v1alpha1.ZarfDistro, error) {
	var arch api.Arch
	if requested != "" {
		parsed, err := api.ParseArch(requested)
		if err != nil {
			return dis, err
		}
		arch = parsed
	}

	if len(dis.Metadata.Architectures) == 0 {
		if dis.Metadata.Architecture == "" {
			dis.Metadata.Architecture = api.Arch(config.GetArch(string(arch)))
			return dis, nil
		}
		if arch != "" && arch != dis.Metadata.Architecture {
			return dis, fmt.Errorf("architecture %q is not targeted by this package, which targets %s",
				arch, dis.Metadata.Architecture)
		}

		return dis, nil
	}

	if arch == "" {
		return dis, nil
	}

	if !slices.Contains(dis.Metadata.Architectures, arch) {
		return dis, fmt.Errorf("architecture %q is not targeted by this package, which targets %s",
			arch, api.FormatArches(dis.Metadata.Architectures))
	}

	dis.Metadata.Architectures = api.Arches{arch}

	return dis, nil
}

func validateDistro(_ context.Context, dis v1alpha1.ZarfDistro, path string) error {
	if err := validateArchitectures(dis); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	return nil
}

// validateArchitectures checks that the package targets a sane set of architectures, and that no
// file selects an architecture outside that set. The second check turns a typo such as x86_64 into
// a build time error rather than a file that silently never uploads.
func validateArchitectures(dis v1alpha1.ZarfDistro) error {
	arches := dis.Metadata.Arches()
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
				return fmt.Errorf("file %q selects architecture %q, but the package targets %s",
					f.Target, arch, api.FormatArches(arches))
			}
		}
	}

	return nil
}
