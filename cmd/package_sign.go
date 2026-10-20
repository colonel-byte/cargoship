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
	"path/filepath"
	"strings"

	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/config/lang"
	"github.com/colonel-byte/cargoship/pkg/distro"
	"github.com/colonel-byte/cargoship/pkg/helpers"
	"github.com/colonel-byte/cargoship/pkg/packager/layout"
	"github.com/colonel-byte/cargoship/pkg/signing"
	"github.com/spf13/cobra"
	zlang "github.com/zarf-dev/zarf/src/config/lang"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"oras.land/oras-go/v2/registry"
)

type packageSignOptions struct {
	signingKeyPath     string
	signingKeyPassword string
	overwrite          bool
	output             string
	ociConcurrency     int
	retries            int

	confirm bool
	packageVerifyFlags
}

func newPackageSignCommand() *cobra.Command {
	o := &packageSignOptions{}

	cmd := &cobra.Command{
		Use:     "sign PACKAGE_SOURCE",
		Aliases: []string{"s"},
		Args:    cobra.ExactArgs(1),
		Short:   lang.CmdDistroSignShort,
		Long:    lang.CmdDistroSignLong,
		Example: lang.CmdDistroSignExample,
		GroupID: lang.RootGroupPackageID,
		PreRunE: o.preRunE,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return o.run(ctx, cmd, args)
		},
	}

	cmd.Flags().StringVar(&o.signingKeyPath, "signing-key", resolvedConfig.DistroOpts.PublishOpts.SigningKey, lang.CmdPackageFlagSigningKey)
	cmd.Flags().StringVar(&o.signingKeyPassword, "signing-key-pass", resolvedConfig.DistroOpts.PublishOpts.SigningKeyPassword, zlang.CmdPackageSignFlagSigningKeyPass)
	cmd.Flags().StringVarP(&o.output, "output", "o", "", zlang.CmdPackageSignFlagOutput)
	cmd.Flags().BoolVar(&o.overwrite, "overwrite", false, zlang.CmdPackageSignFlagOverwrite)
	cmd.Flags().StringVarP(&o.publicKeyPath, "key", "k", resolvedConfig.DistroOpts.PublicKey, zlang.CmdPackageSignFlagKey)
	if err := registerFlagOCIConcurrency(cmd, &o.ociConcurrency); err != nil {
		logger.From(cmd.Context()).Debug("error when trying add shell completion", "error", err)
	}
	cmd.Flags().IntVar(&o.retries, "retries", resolvedConfig.DistroOpts.Retry, lang.CmdPackageFlagRetries)
	o.verify = verifyModeIfPossible
	cmd.Flags().VarP(&o.verify, "verify", "", lang.CmdPackageFlagVerify)

	cmd.Flags().BoolVar(&o.confirm, "confirm", false, zlang.CmdPackageSignFlagConfirm)

	addBuildFlags(cmd)
	addRegistryFlags(cmd)

	return cmd
}

func (o *packageSignOptions) run(ctx context.Context, _ *cobra.Command, args []string) error {
	l := logger.From(ctx)
	packageSource := args[0]

	if o.signingKeyPath == "" {
		return errors.New("--signing-key is required")
	}

	outputDest := o.output
	if outputDest == "" {
		if helpers.IsOCIURL(packageSource) {
			// For OCI sources, default to publishing back to the same OCI location:
			// the repository portion (without the package name/tag) from source.
			trimmed := strings.TrimPrefix(packageSource, helpers.OCIURLPrefix)
			srcRef, err := registry.ParseReference(trimmed)
			if err != nil {
				return fmt.Errorf("failed to parse source OCI reference: %w", err)
			}
			repoParts := strings.Split(srcRef.Repository, "/")
			if len(repoParts) > 1 {
				repoPath := strings.Join(repoParts[:len(repoParts)-1], "/")
				outputDest = helpers.OCIURLPrefix + srcRef.Registry + "/" + repoPath
			} else {
				outputDest = helpers.OCIURLPrefix + srcRef.Registry
			}
		} else {
			// For file sources, use the same directory as the source.
			outputDest = filepath.Dir(packageSource)
		}
	}

	cachePath, err := getCachePath(ctx)
	if err != nil {
		return err
	}

	loadOpts := distro.LoadOptions{
		CachePath:            cachePath,
		Architecture:         config.CLIArch,
		Output:               config.CommonOptions.TempDirectory,
		VerificationStrategy: layout.VerifyNever,
	}

	l.Info("loading package", "source", packageSource)
	distroLayout, err := distro.Load(ctx, packageSource, loadOpts)
	if err != nil {
		return fmt.Errorf("unable to load package: %w", err)
	}
	defer func() {
		if cleanupErr := distroLayout.Cleanup(); cleanupErr != nil {
			l.Warn("failed to cleanup package layout", "error", cleanupErr)
		}
	}()

	signed := distroLayout.IsSigned()

	// To prevent a warning for package not being signed -- we'll only run verification when enforced.
	if signed {
		if o.verify == verifyModeAlways {
			verifyOpts := o.buildVerifyBlobOptions()
			if err := distroLayout.VerifyPackageSignature(ctx, *verifyOpts); err != nil {
				return err
			}
		}
		if !o.overwrite {
			return errors.New("package is already signed, use --overwrite to re-sign")
		}
	}

	l.Info("signing package with provided key")

	signOpts := signing.DefaultSignBlobOptions()
	signOpts.Key = o.signingKeyPath
	signOpts.Password = o.signingKeyPassword
	signOpts.Overwrite = o.overwrite

	if helpers.IsOCIURL(outputDest) {
		dstRef, err := registry.ParseReference(strings.TrimPrefix(outputDest, helpers.OCIURLPrefix))
		if err != nil {
			return fmt.Errorf("invalid destination OCI reference: %w", err)
		}
		l.Info("signing and publishing package to OCI registry", "destination", outputDest)
		_, err = distro.Publish(ctx, distroLayout, dstRef, distro.PublishOptions{
			OCIConcurrency:  o.ociConcurrency,
			Retries:         o.retries,
			SignBlobOptions: signOpts,
			CachePath:       cachePath,
			RemoteOptions:   defaultRemoteOptions(),
			Registry:        &dstRef,
		})
		return err
	}

	if err := distroLayout.SignPackage(ctx, layout.SignOptions{SignBlobOptions: signOpts}); err != nil {
		return fmt.Errorf("failed to sign package: %w", err)
	}

	l.Info("archiving signed package to local directory", "directory", outputDest)
	signedPath, err := distroLayout.Archive(ctx, outputDest, 0)
	if err != nil {
		return fmt.Errorf("failed to archive signed package: %w", err)
	}

	l.Info("package signed successfully", "path", signedPath)
	return nil
}
