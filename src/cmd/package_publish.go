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
	"strings"

	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/config/lang"
	"github.com/colonel-byte/cargoship/src/pkg/distro"
	"github.com/colonel-byte/cargoship/src/pkg/helpers"
	"github.com/colonel-byte/cargoship/src/pkg/lint"
	"github.com/colonel-byte/cargoship/src/types"
	"github.com/spf13/cobra"
	zlang "github.com/zarf-dev/zarf/src/config/lang"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"oras.land/oras-go/v2/registry"
)

type packagePublishOptions struct {
	retries int
	// registryOverrides  []string
	ociConcurrency     int
	signingKeyPath     string
	signingKeyPassword string
	confirm            bool
	packageVerifyFlags
}

func newPackagePublishCommand() *cobra.Command {
	o := packagePublishOptions{}
	cmd := &cobra.Command{
		Use:     "publish [Package] [REPOSITORY]",
		Args:    cobra.ExactArgs(2),
		Short:   lang.CmdDistroPublishShort,
		GroupID: lang.RootGroupPackageID,
		PreRunE: o.preRunE,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return o.run(ctx, args)
		},
	}

	cmd.Flags().BoolVarP(&o.confirm, "confirm", "c", false, zlang.CmdPackagePublishFlagConfirm)
	cmd.Flags().IntVar(&o.ociConcurrency, "oci-concurrency", v.GetInt(types.DistroOCIConcurrency), lang.CmdPackageFlagConcurrency)
	cmd.Flags().IntVar(&o.retries, "retries", v.GetInt(types.DistroRetry), lang.CmdPackageFlagRetries)
	cmd.Flags().StringVar(&o.signingKeyPath, "signing-key", v.GetString(types.DistroPublishSigningKey), zlang.CmdPackagePublishFlagSigningKey)
	cmd.Flags().StringVar(&o.signingKeyPassword, "signing-key-pass", v.GetString(types.DistroPublishSigningKeyPassword), zlang.CmdPackagePublishFlagSigningKeyPassword)
	addVerifyFlags(cmd, v, &o.packageVerifyFlags)

	return cmd
}

func (o *packagePublishOptions) run(ctx context.Context, args []string) error {
	l := logger.From(ctx)
	distroSource := args[0]

	if !helpers.IsOCIURL(args[1]) {
		return errors.New("registry must be prefixed with 'oci://'")
	}

	// Destination Repository
	parts := strings.Split(strings.TrimPrefix(args[1], helpers.OCIURLPrefix), "/")
	dstRef := registry.Reference{
		Registry:   parts[0],
		Repository: strings.Join(parts[1:], "/"),
	}
	err := dstRef.ValidateRegistry()
	if err != nil {
		return err
	}

	cachePath, err := getCachePath(ctx)
	if err != nil {
		return err
	}

	loadOpts := distro.LoadOptions{
		CachePath:    config.CommonOptions.CachePath,
		Architecture: config.CLIArch,
		Output:       config.CommonOptions.TempDirectory,
	}

	distroLayout, err := distro.Load(ctx, distroSource, loadOpts)
	if err != nil {
		return err
	}

	opt := distro.PublishOptions{
		CachePath:      cachePath,
		IsInteractive:  !o.confirm,
		OCIConcurrency: o.ociConcurrency,
		RemoteOptions:  defaultRemoteOptions(),
		Registry:       &dstRef,
	}

	disPath, err := distro.Publish(ctx, distroLayout, dstRef, opt)
	if lintErr, ok := errors.AsType[*lint.LintError](err); ok {
		PrintFindings(ctx, lintErr)
	}
	if err != nil {
		return fmt.Errorf("failed to publish distro package: %w", err)
	}
	l.Debug("distro package published", "path", disPath)

	return nil
}
