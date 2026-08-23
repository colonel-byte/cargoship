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
	"os"

	"github.com/colonel-byte/cargoship/src/config/lang"
	"github.com/colonel-byte/cargoship/src/pkg/distro"
	"github.com/spf13/cobra"
	zconfig "github.com/zarf-dev/zarf/src/config"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type packagePullOptions struct {
	shasum          string
	outputDirectory string
	ociConcurrency  int
	packageVerifyFlags
}

func newPackagePullCommand() *cobra.Command {
	o := packagePullOptions{}
	cmd := &cobra.Command{
		Use:     "pull [Package]",
		Args:    cobra.ExactArgs(1),
		Short:   lang.CmdPackagePullShort,
		GroupID: lang.RootGroupPackageID,
		PreRunE: o.preRunE,
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

	cmd.Flags().StringVar(&o.shasum, "shasum", "", lang.CmdPackagePullFlagShasum)
	cmd.Flags().StringVarP(&o.outputDirectory, "output", "o", output, lang.CmdPackageCreateFlagOutput)
	addVerifyFlags(cmd, v, &o.packageVerifyFlags)

	if err := registerFlagOCIConcurrency(cmd, &o.ociConcurrency); err != nil {
		logger.From(cmd.Context()).Debug("error when trying add shell completion", "error", err)
	}

	return cmd
}

func (o *packagePullOptions) run(ctx context.Context, args []string) error {
	srcURL := args[0]
	outputDir := o.outputDirectory
	if outputDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		outputDir = wd
	}
	cachePath, err := getCachePath(ctx)
	if err != nil {
		return err
	}
	packagePath, err := distro.Pull(ctx, srcURL, outputDir, distro.PullOptions{
		SHASum:               o.shasum,
		VerificationStrategy: o.verify.toStrategy(),
		VerifyBlobOptions:    o.buildVerifyBlobOptions(nil, v),
		Architecture:         zconfig.GetArch(),
		OCIConcurrency:       o.ociConcurrency,
		RemoteOptions:        defaultRemoteOptions(),
		CachePath:            cachePath,
	})
	if err != nil {
		return err
	}
	logger.From(ctx).Info("distro package downloaded successful", "path", packagePath)
	return nil
}
