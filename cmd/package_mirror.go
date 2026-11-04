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
	"net/url"
	"strings"

	clusterapi "github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/config/lang"
	"github.com/colonel-byte/cargoship/pkg/distro"
	"github.com/colonel-byte/cargoship/pkg/helpers"
	"github.com/spf13/cobra"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

type packageMirrorOptions struct {
	config         string
	retries        int
	ociConcurrency int
	images         bool
	files          bool
	packageVerifyFlags
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

	cmd.Flags().StringVarP(&o.config, InstallConfig, "f", "", lang.CmdPackageMirrorFlagConfig)
	cmd.Flags().IntVar(&o.retries, "retries", resolvedConfig.DistroOpts.Retry, lang.CmdPackageFlagRetries)
	cmd.Flags().BoolVar(&o.images, "images", false, lang.CmdPackageMirrorFlagImages)
	cmd.Flags().BoolVar(&o.files, "files", false, lang.CmdPackageMirrorFlagFiles)
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
	target, err := o.resolveTarget(ctx, args)
	if err != nil {
		return err
	}

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

	return distro.Mirror(ctx, distroLayout, target, distro.MirrorOptions{
		RemoteOptions:  defaultRemoteOptions(),
		OCIConcurrency: o.ociConcurrency,
		Retries:        o.retries,
		Images:         o.images,
		Files:          o.files,
	})
}

// resolveTarget decides which registry the package's contents are pushed to: the second argument
// when one was given, and otherwise the mirror the cluster configuration already declares.
//
// Reading it from the cluster configuration is the point rather than a convenience. The address
// has to match what a node will ask for, because cargoship writes that same proxy address into
// each host's containerd hosts.toml, and a mirror populated at a different address is one no node
// ever reaches. Taking both from one field is what makes them agree.
func (o *packageMirrorOptions) resolveTarget(ctx context.Context, args []string) (string, error) {
	if len(args) == 2 {
		if o.config != "" {
			return "", fmt.Errorf("pass either a repository argument or --%s, not both", InstallConfig)
		}
		if !helpers.IsOCIURL(args[1]) {
			return "", errors.New("registry must be prefixed with 'oci://'")
		}
		return strings.TrimPrefix(args[1], helpers.OCIURLPrefix), nil
	}

	if o.config == "" {
		return "", fmt.Errorf("no destination registry: pass one as oci://REGISTRY, or --%s a cluster configuration that declares a registry proxy", InstallConfig)
	}
	cluster, err := initCluster(ctx, InstallCommon{config: o.config})
	if err != nil {
		return "", err
	}
	return mirrorTargetFromCluster(cluster)
}

// mirrorTargetFromCluster picks the one proxied registry in a cluster configuration and returns
// its address in the form a push takes: a host, and the repository prefix when the proxy carries
// a path. The scheme goes, because a reference has no room for one.
func mirrorTargetFromCluster(c clusterapi.Cluster) (string, error) {
	var found []string
	for _, reg := range c.Spec.Config.Registries {
		if endpoint := reg.MirrorEndpoint(); endpoint != "" {
			found = append(found, endpoint)
		}
	}

	switch len(found) {
	case 0:
		return "", fmt.Errorf("the cluster configuration declares no registry proxy to mirror into; add one under spec.config.registries[].proxy.url, or name a registry as oci://REGISTRY")
	case 1:
	default:
		// Which of several mirrors a package belongs in is not a question this command can
		// answer, and guessing it would populate the wrong one silently.
		return "", fmt.Errorf("the cluster configuration declares %d registry proxies (%s); name the one to mirror into as oci://REGISTRY", len(found), strings.Join(found, ", "))
	}

	u, err := url.Parse(found[0])
	if err != nil {
		return "", fmt.Errorf("unable to read the registry proxy address %q: %w", found[0], err)
	}
	if u.Host == "" {
		return "", fmt.Errorf("the registry proxy address %q names no host", found[0])
	}
	return u.Host + strings.TrimSuffix(u.Path, "/"), nil
}
