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
	"strings"

	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/config/lang"
	"github.com/colonel-byte/cargoship/pkg/distro"
	"github.com/colonel-byte/cargoship/pkg/packager/layout"
	"github.com/colonel-byte/cargoship/pkg/signing"
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/logger"
	"oras.land/oras-go/v2/registry"
)

type packageMirrorOptions struct {
	retries        int
	ociConcurrency int
	images         bool
	files          bool
	noRecord       bool
	registryTargetFlags
	packageVerifyFlags
	// publish pushes the package itself alongside the mirrored content. The two answer different
	// questions -- what a node pulls at runtime, and what a consumer pulls to install with -- and
	// a registry holding only the first is one a cargoship consumer cannot converge from.
	publish bool
}

func newPackageMirrorCommand() *cobra.Command {
	o := packageMirrorOptions{}
	cmd := &cobra.Command{
		Use:     "mirror [Package] [REPOSITORY]",
		Args:    cobra.RangeArgs(1, 2),
		Short:   lang.CmdPackageMirrorShort,
		Long:    lang.CmdPackageMirrorLong,
		Example: lang.CmdPackageMirrorExample,
		GroupID: lang.RootGroupPackageID,
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.run(cmd.Context(), args)
		},
	}

	cmd.Flags().IntVar(&o.retries, "retries", resolvedConfig.DistroOpts.Retry, lang.CmdPackageFlagRetries)
	cmd.Flags().BoolVar(&o.images, "images", false, lang.CmdPackageMirrorFlagImages)
	cmd.Flags().BoolVar(&o.files, "files", false, lang.CmdPackageMirrorFlagFiles)
	cmd.Flags().BoolVar(&o.noRecord, "no-record", false, lang.CmdPackageMirrorFlagNoRecord)
	cmd.Flags().BoolVar(&o.publish, "publish", false, lang.CmdPackageMirrorFlagPublish)
	addRegistryTargetFlags(cmd, &o.registryTargetFlags)
	cmd.MarkFlagsMutuallyExclusive("images", "files")
	addVerifyFlags(cmd, &o.packageVerifyFlags)

	if err := registerFlagOCIConcurrency(cmd, &o.ociConcurrency); err != nil {
		logger.From(cmd.Context()).Debug("error when trying add shell completion", "error", err)
	}

	addBuildFlags(cmd)
	addRegistryFlags(cmd)

	return cmd
}

func (o *packageMirrorOptions) run(ctx context.Context, args []string) error {
	explicit, err := explicitRegistry(args, 1)
	if err != nil {
		return err
	}
	dst, err := o.resolveTarget(ctx, explicit)
	if err != nil {
		return err
	}
	defer dst.release()

	if err := checkPackageSource(args[0]); err != nil {
		return err
	}

	cachePath, err := getCachePath(ctx)
	if err != nil {
		return err
	}

	distroLayout, err := distro.Load(ctx, args[0], distro.LoadOptions{
		CachePath:            cachePath,
		Architecture:         config.CLIArch,
		Output:               config.CommonOptions.TempDirectory,
		VerificationStrategy: o.verify.toStrategy(),
		VerifyBlobOptions:    o.buildVerifyBlobOptions(),
	})
	if err != nil {
		return err
	}
	defer func() {
		if cerr := distroLayout.Cleanup(); cerr != nil {
			logger.From(ctx).Warn("failed to clean up the extracted package", "error", cerr)
		}
	}()

	if err := distro.Mirror(ctx, distroLayout, dst.registry, distro.MirrorOptions{
		RemoteOptions:  defaultRemoteOptions(),
		OCIConcurrency: o.ociConcurrency,
		Retries:        o.retries,
		Images:         o.images,
		Files:          o.files,
		Username:       dst.username,
		Password:       dst.password,
		DialContext:    dst.dial,
		CABundle:       dst.caBundle,
		NoRecord:       o.noRecord,
	}); err != nil {
		return err
	}

	if !o.publish {
		return nil
	}
	return o.publishPackage(ctx, distroLayout, dst, cachePath)
}

// publishPackage pushes the package itself to the registry the mirror just filled.
//
// After the mirror rather than before it, and only if the mirror succeeded: the artifact this
// writes is what a consumer pulls to install with, and a registry advertising one whose content
// is not there is worse than a registry with neither.
//
// The same transport the mirror used, which is the whole reason PublishOptions carries one: the
// registry may be reachable only through a tunnel, serve a certificate from zarf's own CA, and
// want credentials that exist nowhere but zarf's state.
func (o *packageMirrorOptions) publishPackage(
	ctx context.Context,
	distroLayout *layout.DistroLayout,
	dst registryTarget,
	cachePath string,
) error {
	published, err := distro.Publish(ctx, distroLayout, publishReference(dst.registry), distro.PublishOptions{
		RemoteOptions:   defaultRemoteOptions(),
		OCIConcurrency:  o.ociConcurrency,
		Retries:         o.retries,
		CachePath:       cachePath,
		SignBlobOptions: signing.DefaultSignBlobOptions(),
		Username:        dst.username,
		Password:        dst.password,
		DialContext:     dst.dial,
		CABundle:        dst.caBundle,
		// Both records or neither: --no-record says an operator wants nothing in the record
		// repository, and a published artifact nobody can find is still a published artifact.
		Record: !o.noRecord,
	})
	if err != nil {
		return err
	}
	logger.From(ctx).Info("published the package beside the mirrored content", "reference", published.String())
	return nil
}

// publishReference splits a mirror target into the reference a publish takes: the host, and
// whatever repository prefix the target carried.
//
// A namespace rather than a full reference, because Publish appends the package's own name and
// version to it -- the same reference `cargoship publish` would have written, so a consumer
// resolving a record finds the artifact where the record says.
func publishReference(target string) registry.Reference {
	host, prefix, _ := strings.Cut(strings.TrimSuffix(strings.TrimSpace(target), "/"), "/")
	return registry.Reference{Registry: host, Repository: prefix}
}
