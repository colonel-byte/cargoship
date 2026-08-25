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

	"github.com/colonel-byte/cargoship/src/config/lang"
	"github.com/colonel-byte/cargoship/src/pkg/distro"
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
	skipSBOM           bool
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

	cmd.Flags().BoolVarP(&o.confirm, "confirm", "c", false, zlang.CmdPackagePublishFlagConfirm)
	cmd.Flags().StringVarP(&o.output, "output", "o", output, lang.CmdPackageCreateFlagOutput)
	cmd.Flags().StringSliceVar(&o.registryOverrides, "registry-override", resolvedConfig.DistroOpts.CreateOpts.RegistryOverride, zlang.CmdPackageCreateFlagRegistryOverride)
	cmd.Flags().BoolVar(&o.skipSBOM, "skip-sbom", resolvedConfig.DistroOpts.CreateOpts.SkipSBOM, zlang.CmdPackageCreateFlagSkipSbom)
	cmd.Flags().BoolVar(&o.reproducible, "reproducible", false, lang.CmdPackageCreateFlagReproducible)
	cmd.Flags().StringVar(&o.signingKeyPath, "signing-key", resolvedConfig.DistroOpts.PublishOpts.SigningKey, zlang.CmdPackageCreateFlagSigningKey)
	cmd.Flags().StringVar(&o.signingKeyPassword, "signing-key-pass", resolvedConfig.DistroOpts.PublishOpts.SigningKeyPassword, zlang.CmdPackageCreateFlagSigningKeyPassword)

	if err := registerFlagOCIConcurrency(cmd, &o.ociConcurrency); err != nil {
		logger.From(cmd.Context()).Debug("error when trying add shell completion", "error", err)
	}

	v.SetDefault(distroOutputKey, ".")

	return cmd
}

func (o *packageCreateOptions) run(ctx context.Context, args []string) error {
	l := logger.From(ctx)
	basePath := setBaseDirectory(args)
	cachePath, err := getCachePath(ctx)
	if err != nil {
		return err
	}

	opt := distro.CreateOptions{
		CachePath:          cachePath,
		IsInteractive:      !o.confirm,
		OCIConcurrency:     o.ociConcurrency,
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
