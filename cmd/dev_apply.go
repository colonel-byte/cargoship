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
	"slices"

	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/config/lang"
	"github.com/colonel-byte/cargoship/pkg/distro"
	"github.com/colonel-byte/cargoship/pkg/fileoverride"
	"github.com/colonel-byte/cargoship/pkg/packager/layout"
	"github.com/colonel-byte/cargoship/pkg/phase"
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// devApplyOptions is installApplyOptions plus the build-side settings, because this command does
// both halves. Only the settings that change what gets built are carried over from create:
// --output, --reproducible, --tag and the signing flags all describe an artifact this command
// never produces.
type devApplyOptions struct {
	installApplyOptions
	registryOverrides []string
	fileOverrides     []string
	contentStore      string
	ociConcurrency    int
}

func newDevApplyCommand() *cobra.Command {
	o := devApplyOptions{}
	cmd := &cobra.Command{
		Use:     "apply [Dir]",
		Args:    cobra.MaximumNArgs(1),
		Short:   lang.CmdDevApplyShort,
		Long:    lang.CmdDevApplyLong,
		Example: lang.CmdDevApplyExample,
		PreRunE: o.preRunE,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// The shared run reads one positional argument, so the directory default that
			// create applies is resolved here rather than inside it.
			return o.run(ctx, cmd, []string{setBaseDirectory(args)})
		},
	}

	o.source = packageSource{
		preflight: preflightDevApply,
		open:      o.buildManager,
	}

	addInstallApplyFlags(cmd, &o.installApplyOptions)

	registryOverrideDefaults := make([]string, 0, len(resolvedConfig.DistroOpts.CreateOpts.RegistryOverride))
	for source, override := range resolvedConfig.DistroOpts.CreateOpts.RegistryOverride {
		registryOverrideDefaults = append(registryOverrideDefaults, source+"="+override)
	}
	slices.Sort(registryOverrideDefaults)

	fileOverrideDefaults := make([]string, 0, len(resolvedConfig.DistroOpts.CreateOpts.FileOverride))
	for source, override := range resolvedConfig.DistroOpts.CreateOpts.FileOverride {
		fileOverrideDefaults = append(fileOverrideDefaults, source+"="+override)
	}
	slices.Sort(fileOverrideDefaults)

	cmd.Flags().StringSliceVar(&o.fileOverrides, "file-override", fileOverrideDefaults, lang.CmdPackageCreateFlagFileOverride)
	cmd.Flags().StringVar(&o.contentStore, "content-store", resolvedConfig.DistroOpts.CreateOpts.ContentStore, lang.CmdPackageCreateFlagContentStore)
	cmd.Flags().StringSliceVar(&o.registryOverrides, "registry-override", registryOverrideDefaults, lang.CmdPackageCreateFlagRegistryOverride)

	// apply does not register these, because loading a built package reaches no registry.
	// This command pulls the images itself, so it needs the same transport flags create has.
	addRegistryFlags(cmd)

	if err := registerFlagOCIConcurrency(cmd, &o.ociConcurrency); err != nil {
		logger.From(cmd.Context()).Debug("error when trying add shell completion", "error", err)
	}

	return cmd
}

// buildManager assembles the definition at distroPath and resolves the resulting layout against
// the cluster inventory, in place of loading a package that was built earlier.
//
// Nothing is archived and nothing is reloaded, which is the whole point: a build directory this
// process just wrote needs neither the compress step nor the integrity and signature checks that
// exist to vet bytes which travelled. It goes to the same managerFromLayout a real apply uses, so
// values resolution, engine-config mapping and template rendering are identical.
func (o *devApplyOptions) buildManager(ctx context.Context, distroPath string, opt InstallCommon) (*phase.Manager, *layout.DistroLayout, error) {
	cluster, err := initCluster(ctx, opt)
	if err != nil {
		return nil, nil, err
	}

	cachePath, err := getCachePath(ctx)
	if err != nil {
		return nil, nil, err
	}

	registryOverrides, err := ParseRegistryOverrides(o.registryOverrides)
	if err != nil {
		return nil, nil, err
	}
	fileOverrides, err := fileoverride.Parse(o.fileOverrides)
	if err != nil {
		return nil, nil, err
	}

	logger.From(ctx).Info("building the distro definition in place of loading a package", "definition", distroPath)

	distroLayout, err := distro.Build(ctx, distroPath, distro.CreateOptions{
		OCIConcurrency:    o.ociConcurrency,
		CachePath:         cachePath,
		Architecture:      config.CLIArch,
		RegistryOverrides: registryOverrides,
		FileOverrides:     fileOverrides,
		ContentStore:      o.contentStore,
		RemoteOptions:     defaultRemoteOptions(),
	})
	if err != nil {
		return nil, nil, err
	}

	// The layout is not cleaned up here: run removes manager.TempDirectory, which is this
	// layout's directory, once the phases are done.
	manager, err := managerFromLayout(ctx, cluster, distroLayout, opt)
	if err != nil {
		return nil, nil, err
	}
	return manager, distroLayout, nil
}
