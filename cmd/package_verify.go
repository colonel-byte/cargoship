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

	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/config/lang"
	"github.com/colonel-byte/cargoship/pkg/distro"
	"github.com/colonel-byte/cargoship/pkg/packager/layout"
	"github.com/spf13/cobra"
	zlang "github.com/zarf-dev/zarf/src/config/lang"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type packageVerifyOptions struct {
	packageVerifyFlags
}

func newPackageVerifyCommand() *cobra.Command {
	o := &packageVerifyOptions{}

	cmd := &cobra.Command{
		Use:     "verify PACKAGE_SOURCE",
		Args:    cobra.ExactArgs(1),
		Short:   lang.CmdDistroVerifyShort,
		Long:    lang.CmdDistroVerifyLong,
		Example: lang.CmdDistroVerifyExample,
		GroupID: lang.RootGroupPackageID,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return o.run(ctx, cmd, args)
		},
	}

	// Verification is this command's whole job, so there is no --verify mode flag to
	// opt out of it. --key is registered here rather than through addVerifyFlags for
	// the same reason: the full set would carry --verify along with it.
	cmd.Flags().StringVarP(&o.publicKeyPath, "key", "k", resolvedConfig.DistroOpts.PublicKey, zlang.CmdPackageFlagFlagPublicKey)
	cmd.Flags().AddFlagSet(newKeylessVerifyFlagSet(v, &o.packageVerifyFlags))
	markVerifyFlagsMutuallyExclusive(cmd)

	addRegistryFlags(cmd)

	return cmd
}

func (o *packageVerifyOptions) run(ctx context.Context, cmd *cobra.Command, args []string) error {
	l := logger.From(ctx)
	packageSource := args[0]

	cachePath, err := getCachePath(ctx)
	if err != nil {
		return err
	}

	// Load with VerifyNever so an unsigned or badly signed package still loads and the
	// failure comes from VerifyPackageSignature below, where the message is specific.
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

	// Unlike sign, an unsigned package is a failure rather than a warning: the operator
	// asked whether the signature is good, and "there is no signature" is not a yes.
	if !distroLayout.IsSigned() {
		return errors.New("package is not signed, so there is no signature to verify")
	}

	verifyOpts := o.buildVerifyBlobOptions(cmd, v)
	if err := distroLayout.VerifyPackageSignature(ctx, *verifyOpts); err != nil {
		return err
	}

	l.Info("package signature verified successfully", "source", packageSource)
	return nil
}
