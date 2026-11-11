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
	"github.com/colonel-byte/cargoship/cmd/flags"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/config/lang"
	"github.com/colonel-byte/cargoship/internal/zarfstate"
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
	fromZarfState  bool
	kubeContext    string
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
	cmd.Flags().BoolVar(&o.fromZarfState, "from-zarf-state", false, lang.CmdPackageMirrorFlagFromZarfState)
	cmd.Flags().StringVar(&o.kubeContext, "context", "", lang.CmdPackageMirrorFlagContext)
	if err := cmd.RegisterFlagCompletionFunc("context", flags.RegisterKubeContext); err != nil {
		logger.From(cmd.Context()).Debug("error when trying add shell completion", "error", err)
	}
	cmd.MarkFlagsMutuallyExclusive("images", "files")
	cmd.MarkFlagsMutuallyExclusive(InstallConfig, "from-zarf-state")
	addVerifyFlags(cmd, &o.packageVerifyFlags)

	if err := registerFlagOCIConcurrency(cmd, &o.ociConcurrency); err != nil {
		logger.From(cmd.Context()).Debug("error when trying add shell completion", "error", err)
	}

	addBuildFlags(cmd)
	addRegistryFlags(cmd)

	return cmd
}

func (o *packageMirrorOptions) run(ctx context.Context, args []string) error {
	dst, err := o.resolveTarget(ctx, args)
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

	return distro.Mirror(ctx, distroLayout, dst.registry, distro.MirrorOptions{
		RemoteOptions:  defaultRemoteOptions(),
		OCIConcurrency: o.ociConcurrency,
		Retries:        o.retries,
		Images:         o.images,
		Files:          o.files,
		Username:       dst.username,
		Password:       dst.password,
	})
}

// mirrorTarget is where a mirror pushes, and the credentials it authenticates with when they
// came from somewhere that knows them. Empty credentials fall back to the Docker configuration,
// which is how every other cargoship registry operation authenticates.
type mirrorTarget struct {
	registry string
	username string
	password string
}

// resolveTarget decides which registry the package's contents are pushed to, and with which
// credentials: the second argument when one was given, the Zarf deployment's own registry under
// --from-zarf-state, and otherwise the mirror the cluster configuration already declares.
//
// Reading it from the cluster configuration is the point rather than a convenience. The address
// has to match what a node will ask for, because cargoship writes that same proxy address into
// each host's containerd hosts.toml, and a mirror populated at a different address is one no node
// ever reaches. Taking both from one field is what makes them agree.
func (o *packageMirrorOptions) resolveTarget(ctx context.Context, args []string) (mirrorTarget, error) {
	var explicit string
	if len(args) == 2 {
		if !helpers.IsOCIURL(args[1]) {
			return mirrorTarget{}, errors.New("registry must be prefixed with 'oci://'")
		}
		explicit = strings.TrimPrefix(args[1], helpers.OCIURLPrefix)
	}

	if o.fromZarfState {
		return o.zarfStateTarget(ctx, explicit)
	}

	// Nothing else reads a cluster, so a context named here would be silently ignored rather
	// than wrong -- which is worse, because it looks like it was honoured.
	if o.kubeContext != "" {
		return mirrorTarget{}, errors.New("--context selects which cluster the zarf state is read from, so it needs --from-zarf-state")
	}

	if explicit != "" {
		if o.config != "" {
			return mirrorTarget{}, fmt.Errorf("pass either a repository argument or --%s, not both", InstallConfig)
		}
		return mirrorTarget{registry: explicit}, nil
	}

	if o.config == "" {
		return mirrorTarget{}, fmt.Errorf("no destination registry: pass one as oci://REGISTRY, --from-zarf-state to read a zarf deployment's own registry, or --%s a cluster configuration that declares a registry proxy", InstallConfig)
	}
	cluster, err := initCluster(ctx, InstallCommon{config: o.config})
	if err != nil {
		return mirrorTarget{}, err
	}
	registry, err := mirrorTargetFromCluster(cluster)
	if err != nil {
		return mirrorTarget{}, err
	}
	return mirrorTarget{registry: registry}, nil
}

// zarfStateTarget reads the destination out of a Zarf deployment's state secret. The push
// credentials always come from there -- that is the half of this worth having, since they are
// generated by Zarf and written nowhere an operator would otherwise find them.
//
// The address only comes from there when the registry is external, because that is the one mode
// whose recorded address is routable from a management node. Zarf's internal registry is reached
// through a NodePort or a per-node host port, and Zarf tunnels to it; cargoship has no tunnel,
// so the honest thing is to say so and take the address from the operator, who knows which node
// to ask. An explicit oci://REGISTRY always wins, and pairing it with --from-zarf-state is the
// normal way to mirror into an internal registry.
func (o *packageMirrorOptions) zarfStateTarget(ctx context.Context, explicit string) (mirrorTarget, error) {
	reg, err := zarfstate.Load(ctx, resolvedConfig.DistroOpts.KubeConfig, o.kubeContext)
	if err != nil {
		return mirrorTarget{}, err
	}
	return targetFromZarfRegistry(reg, explicit)
}

// targetFromZarfRegistry is the decision zarfStateTarget makes once the state has been read,
// split out so it can be exercised without a cluster.
func targetFromZarfRegistry(reg zarfstate.Registry, explicit string) (mirrorTarget, error) {
	if reg.UsesMTLS() {
		return mirrorTarget{}, fmt.Errorf("the zarf registry is configured for zarf-managed mTLS (%s), and a push with a client certificate is not supported", reg.MTLSStrategy)
	}

	dst := mirrorTarget{
		registry: explicit,
		username: reg.PushUsername,
		password: reg.PushPassword,
	}
	if dst.registry != "" {
		return dst, nil
	}

	if !reg.IsExternal() {
		return mirrorTarget{}, fmt.Errorf("the zarf registry runs in the cluster (mode %q, recorded as %q), which is an address only a node can reach; name the address this machine reaches it on as oci://REGISTRY -- a node address and port %d -- and keep --from-zarf-state for the credentials", reg.Mode, reg.Address, reg.Port)
	}
	if reg.Address == "" {
		return mirrorTarget{}, errors.New("the zarf state records no registry address; name one as oci://REGISTRY")
	}
	dst.registry = reg.Address
	return dst, nil
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
