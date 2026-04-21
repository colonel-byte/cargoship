// Copyright 2026 colonel-byte
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

	"github.com/colonel-byte/mare/src/config/lang"
	"github.com/colonel-byte/mare/src/pkg/distro"
	"github.com/spf13/cobra"
	zcmd "github.com/zarf-dev/zarf/src/cmd"
	zconfig "github.com/zarf-dev/zarf/src/config"
	zlang "github.com/zarf-dev/zarf/src/config/lang"
	"github.com/zarf-dev/zarf/src/pkg/lint"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type packageCreateOptions struct {
	output            string
	registryOverrides []string
	ociConcurrency    int
	confirm           bool
	skipSBOM          bool
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

	output, err := zconfig.GetAbsHomePath(v.GetString(VDistroCreateOutput))
	if err != nil {
		logger.From(cmd.Context()).Debug("error when trying to get user path", "error", err)
		output = v.GetString(VDistroCreateOutput)
	}

	cmd.Flags().IntVar(&o.ociConcurrency, "oci-concurrency", v.GetInt(VDistroOCIConcurrency), lang.CmdPackageFlagConcurrency)
	cmd.Flags().StringVarP(&o.output, "output", "o", output, lang.CmdPackageCreateFlagOutput)
	cmd.Flags().StringSliceVar(&o.registryOverrides, "registry-override", zcmd.GetStringSlice(v, VDistroCreateRegistryOverride), zlang.CmdPackageCreateFlagRegistryOverride)
	cmd.Flags().BoolVar(&o.skipSBOM, "skip-sbom", v.GetBool(VDistroCreateSkipSbom), zlang.CmdPackageCreateFlagSkipSbom)

	v.SetDefault(VDistroCreateOutput, ".")

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
		//keep-sorted start
		CachePath:      cachePath,
		IsInteractive:  !o.confirm,
		OCIConcurrency: o.ociConcurrency,
		RemoteOptions:  defaultRemoteOptions(),
		//keep-sorted end
	}

	disPath, err := distro.Create(ctx, basePath, o.output, opt)
	if lintErr, ok := errors.AsType[*lint.LintError](err); ok {
		zcmd.PrintFindings(ctx, lintErr)
	}
	if err != nil {
		return fmt.Errorf("failed to create distro package: %w", err)
	}
	l.Debug("distro package created", "path", disPath)

	return nil
}
