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

package cmd

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/colonel-byte/cargoship/src/config/lang"
	"github.com/colonel-byte/cargoship/src/pkg/distro"
	"github.com/colonel-byte/cargoship/src/pkg/images"
	"github.com/colonel-byte/cargoship/src/pkg/lint"
	"github.com/spf13/cobra"
	zconfig "github.com/zarf-dev/zarf/src/config"
	zlang "github.com/zarf-dev/zarf/src/config/lang"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// distroOutputKey is read directly from viper (not resolvedConfig), in both this file
// and package_pull.go: the SetDefault(".") below runs after resolvedConfig's one-time
// Unmarshal (which happens early, inside initViper()), so a struct field would never
// see it. Still derived via configPath (see viper.go) so the key can't drift from the
// struct's own tag.
var distroOutputKey = configPath("DistroOpts", "Output")

type packageCreateOptions struct {
	output             string
	registryOverrides  []string
	ociConcurrency     int
	confirm            bool
	reproducible       bool
	signingKeyPath     string
	signingKeyPassword string
}

func newPackageCreateCommand() *cobra.Command {
	o := packageCreateOptions{}
	cmd := &cobra.Command{
		Use:     "create [Dir]",
		Args:    cobra.MaximumNArgs(1),
		Short:   lang.CmdDistroCreateShort,
		Example: lang.CmdDistroCreateExample,
		GroupID: lang.RootGroupPackageID,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return o.run(ctx, args)
		},
	}

	output, err := zconfig.GetAbsHomePath(v.GetString(distroOutputKey))
	if err != nil {
		logger.From(cmd.Context()).Debug("error when trying to get user path", "error", err)
		output = v.GetString(distroOutputKey)
	}

	registryOverrideDefaults := make([]string, 0, len(resolvedConfig.DistroOpts.CreateOpts.RegistryOverride))
	for source, override := range resolvedConfig.DistroOpts.CreateOpts.RegistryOverride {
		registryOverrideDefaults = append(registryOverrideDefaults, source+"="+override)
	}
	slices.Sort(registryOverrideDefaults)

	cmd.Flags().BoolVarP(&o.confirm, "confirm", "c", false, zlang.CmdPackagePublishFlagConfirm)
	cmd.Flags().StringVarP(&o.output, "output", "o", output, lang.CmdPackageCreateFlagOutput)
	cmd.Flags().StringSliceVar(&o.registryOverrides, "registry-override", registryOverrideDefaults, zlang.CmdPackageCreateFlagRegistryOverride)
	cmd.Flags().BoolVar(&o.reproducible, "reproducible", false, lang.CmdPackageCreateFlagReproducible)
	cmd.Flags().StringVar(&o.signingKeyPath, "signing-key", resolvedConfig.DistroOpts.PublishOpts.SigningKey, zlang.CmdPackageCreateFlagSigningKey)
	cmd.Flags().StringVar(&o.signingKeyPassword, "signing-key-pass", resolvedConfig.DistroOpts.PublishOpts.SigningKeyPassword, zlang.CmdPackageCreateFlagSigningKeyPassword)

	if err := registerFlagOCIConcurrency(cmd, &o.ociConcurrency); err != nil {
		logger.From(cmd.Context()).Debug("error when trying add shell completion", "error", err)
	}

	v.SetDefault(distroOutputKey, ".")

	return cmd
}

// parseRegistryOverrides converts registry overrides to a structured type.
// The result is sorted in descending order by Source, which guarantees the
// longest prefix will be sorted toward the beginning -- so that e.g.
// docker.io/library overrides win over a broader docker.io override.
//
// Input is of the following form:
// []string{"docker.io/library=docker.example.com", "docker.io=docker.example.com"}
func parseRegistryOverrides(overrides []string) ([]images.RegistryOverride, error) {
	result := make([]images.RegistryOverride, len(overrides))
	for i, mapping := range overrides {
		source, override, found := strings.Cut(mapping, "=")
		if !found {
			return nil, fmt.Errorf("registry override missing '=': %s", mapping)
		}
		if source == "" {
			return nil, fmt.Errorf("registry override missing source: %s", mapping)
		}
		if override == "" {
			return nil, fmt.Errorf("registry override missing value: %s", mapping)
		}
		if index := slices.IndexFunc(result, func(existing images.RegistryOverride) bool {
			return existing.Source == source
		}); index >= 0 {
			return nil, fmt.Errorf("registry override has duplicate source: existing index %d, new index %d, source %s", index, i, source)
		}
		result[i].Source = source
		result[i].Override = override
	}

	slices.SortFunc(result, func(a images.RegistryOverride, b images.RegistryOverride) int {
		return -strings.Compare(a.Source, b.Source)
	})

	return result, nil
}

func (o *packageCreateOptions) run(ctx context.Context, args []string) error {
	l := logger.From(ctx)
	basePath := setBaseDirectory(args)
	cachePath, err := getCachePath(ctx)
	if err != nil {
		return err
	}

	registryOverrides, err := parseRegistryOverrides(o.registryOverrides)
	if err != nil {
		return fmt.Errorf("error parsing registry override: %w", err)
	}
	l.Debug("parsed registry overrides", "overrides", registryOverrides)

	opt := distro.CreateOptions{
		CachePath:          cachePath,
		IsInteractive:      !o.confirm,
		OCIConcurrency:     o.ociConcurrency,
		RegistryOverrides:  registryOverrides,
		RemoteOptions:      defaultRemoteOptions(),
		Reproducible:       o.reproducible,
		SigningKeyPath:     o.signingKeyPath,
		SigningKeyPassword: o.signingKeyPassword,
	}

	disPath, err := distro.Create(ctx, basePath, o.output, opt)
	if lintErr, ok := errors.AsType[*lint.LintError](err); ok {
		PrintFindings(ctx, lintErr)
	}
	if err != nil {
		return fmt.Errorf("failed to create distro package: %w", err)
	}
	l.Debug("distro package created", "path", disPath)

	return nil
}
