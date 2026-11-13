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
	"net"
	"net/url"
	"strconv"
	"strings"

	clusterapi "github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/cmd/flags"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/config/lang"
	"github.com/colonel-byte/cargoship/internal/zarfstate"
	"github.com/colonel-byte/cargoship/pkg/distro"
	"github.com/colonel-byte/cargoship/pkg/helpers"
	"github.com/colonel-byte/cargoship/pkg/phase"
	"github.com/colonel-byte/cargoship/pkg/tunnel"
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

	return distro.Mirror(ctx, distroLayout, dst.registry, distro.MirrorOptions{
		RemoteOptions:  defaultRemoteOptions(),
		OCIConcurrency: o.ociConcurrency,
		Retries:        o.retries,
		Images:         o.images,
		Files:          o.files,
		Username:       dst.username,
		Password:       dst.password,
		DialContext:    dst.dial,
		CABundle:       dst.caBundle,
	})
}

// mirrorTarget is where a mirror pushes, and the credentials it authenticates with when they
// came from somewhere that knows them. Empty credentials fall back to the Docker configuration,
// which is how every other cargoship registry operation authenticates.
type mirrorTarget struct {
	registry string
	username string
	password string
	// dial carries the push when the registry is one only a cluster node can reach, and close
	// releases whatever it is carried over. Both are nil for a registry this machine can dial.
	dial  func(ctx context.Context, network, addr string) (net.Conn, error)
	close func()
	// caBundle is the CA the registry's server certificate is validated against, set when the
	// deployment manages its own. Nil for a registry whose certificate the system roots cover.
	caBundle []byte
}

// apiDial is the route to the API server: a tunnel's dialer, or nil for a direct connection.
type apiDial func(ctx context.Context, network, addr string) (net.Conn, error)

// release closes a tunnel the target opened, and is safe on a target that has none.
func (t mirrorTarget) release() {
	if t.close != nil {
		t.close()
	}
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
	// With a cluster configuration, one SSH connection to a controller serves everything that
	// follows: the API server is read through it to find the registry, and then the push itself
	// travels through it. Opening it first is what makes a fleet reachable only over SSH work at
	// all, because the state read would otherwise need the API port open.
	if o.config != "" {
		return o.tunnelledTarget(ctx, explicit)
	}

	reg, err := zarfstate.Load(ctx, zarfstate.Options{
		Kubeconfig: resolvedConfig.DistroOpts.KubeConfig,
		Context:    o.kubeContext,
	})
	if err != nil {
		return mirrorTarget{}, err
	}
	return o.targetFromZarfRegistry(ctx, reg, explicit)
}

// registryCABundle reads the CA a zarf-managed-mTLS registry's certificate is signed by, and
// returns nil for a deployment that manages no certificates of its own.
//
// dial is the route to the API server, which is the tunnel's dialer under --config and nil
// otherwise. Reading the CA over the same route as the state is what keeps this working on a
// fleet whose API port is closed: the two reads are the same cluster, reached the same way.
func (o *packageMirrorOptions) registryCABundle(ctx context.Context, reg zarfstate.Registry, dial apiDial) ([]byte, error) {
	if !reg.UsesMTLS() {
		return nil, nil
	}
	ca, err := zarfstate.LoadCABundle(ctx, zarfstate.Options{
		Kubeconfig: resolvedConfig.DistroOpts.KubeConfig,
		Context:    o.kubeContext,
		Dial:       dial,
	})
	if err != nil {
		return nil, fmt.Errorf("the zarf registry manages its own certificates (mTLS strategy %q), so its CA is needed to verify the push, and reading it failed: %w -- pass --insecure-skip-tls-verify to push without verifying instead", reg.MTLSStrategy, err)
	}
	logger.From(ctx).Debug("trusting the zarf registry's own certificate authority", "mtlsStrategy", reg.MTLSStrategy, "bytes", len(ca))
	return ca, nil
}

// registryAddress decides what address a mirror pushes to, and whether only a cluster node can
// reach it. It is pure, and it is where every rule about reading a destination out of zarf state
// lives.
//
// An explicit oci:// argument always wins. Failing that, the state's own address is used when the
// registry is external, which is the one mode whose recorded address is routable from somewhere
// other than a node. Anything else is node-local, and the caller decides whether it holds a
// tunnel that can reach it.
//
// A zarf-managed-mTLS registry is not refused here, and used to be. The refusal said a push
// would need a client certificate, and that premise is wrong: zarf issues a client certificate
// for the per-node registry proxy, not for a direct client, and the registry accepts a push
// authenticated with the push credentials alone. What mTLS does change is the scheme and the
// trust -- it is always HTTPS, with a server certificate from a private CA - which is the
// caller's business rather than this function's. See zarfstate.LoadCABundle.
func registryAddress(reg zarfstate.Registry, explicit string) (addr string, nodeLocal bool, err error) {
	if explicit != "" {
		return explicit, false, nil
	}
	if reg.IsExternal() {
		if reg.Address == "" {
			return "", false, errors.New("the zarf state records no registry address; name one as oci://REGISTRY")
		}
		return reg.Address, false, nil
	}

	// A node-local address needs a port to be dialled, and older zarf state recorded one only in
	// the deprecated field decode already folds in.
	service := reg.Address
	if _, _, splitErr := net.SplitHostPort(service); splitErr != nil {
		if reg.Port == 0 {
			return "", true, fmt.Errorf("the zarf state records the registry as %q with no port, so there is nothing to reach it on", reg.Address)
		}
		service = net.JoinHostPort(service, strconv.Itoa(reg.Port))
	}
	return service, true, nil
}

// targetFromZarfRegistry builds a destination for a registry this machine can dial itself. A
// node-local one needs the tunnel, which is tunnelledTarget's business.
func (o *packageMirrorOptions) targetFromZarfRegistry(ctx context.Context, reg zarfstate.Registry, explicit string) (mirrorTarget, error) {
	addr, nodeLocal, err := registryAddress(reg, explicit)
	if err != nil {
		return mirrorTarget{}, err
	}
	if nodeLocal {
		return mirrorTarget{}, fmt.Errorf("the zarf registry runs in the cluster (mode %q, recorded as %q), which is an address only a node can reach; either name the address this machine reaches it on as oci://REGISTRY -- a node address and port %d -- or pass --%s so the push is tunnelled through a node. Either way --from-zarf-state supplies the credentials", reg.Mode, reg.Address, reg.Port, InstallConfig)
	}

	ca, err := o.registryCABundle(ctx, reg, nil)
	if err != nil {
		return mirrorTarget{}, err
	}

	return mirrorTarget{
		registry: addr,
		username: reg.PushUsername,
		password: reg.PushPassword,
		caBundle: ca,
	}, nil
}

// tunnelledTarget opens one SSH connection to a controller and uses it for both halves of the
// job: the Kubernetes API is read through it to find the registry, and then the push travels
// through it.
//
// Reading the state through the tunnel rather than over a route of its own is what makes a fleet
// reachable only on port 22 work. The kubeconfig still supplies the identity and the CA -- its
// server URL is left untouched, so TLS validates against the hostname it names while the bytes
// travel through SSH -- and the port to tunnel to is read from that same URL rather than assumed.
//
// For the registry the recorded address does double duty, which is what makes this correct rather
// than merely workable: it is the address every node reaches the registry on, so it is both the
// service the channel opens onto and the reference host the images are pushed under. One value in
// both roles means the mirror cannot be populated at an address a node will not ask for -- the
// same property reading the destination from a cluster configuration has.
func (o *packageMirrorOptions) tunnelledTarget(ctx context.Context, explicit string) (mirrorTarget, error) {
	cluster, err := initCluster(ctx, InstallCommon{config: o.config})
	if err != nil {
		return mirrorTarget{}, err
	}
	host, err := firstControllerWithSSH(cluster)
	if err != nil {
		return mirrorTarget{}, err
	}

	conn, err := tunnel.Open(ctx, host.ConnectionConfig.SSH)
	if err != nil {
		// Joined so that a run that never reached a host is classified the way every other
		// unreachable-fleet failure is. See cmd/exit.go.
		return mirrorTarget{}, errors.Join(phase.ErrConnect, err)
	}
	// The connection outlives this function only on success, where it is handed to the caller as
	// the target's closer.
	handedOver := false
	defer func() {
		if !handedOver {
			conn.Close()
		}
	}()

	reg, apiDial, err := o.zarfStateThrough(ctx, conn)
	if err != nil {
		return mirrorTarget{}, err
	}

	addr, nodeLocal, err := registryAddress(reg, explicit)
	if err != nil {
		return mirrorTarget{}, err
	}

	ca, err := o.registryCABundle(ctx, reg, apiDial)
	if err != nil {
		return mirrorTarget{}, err
	}

	dst := mirrorTarget{
		registry: addr,
		username: reg.PushUsername,
		password: reg.PushPassword,
		caBundle: ca,
	}
	if !nodeLocal {
		// The destination is one this machine can reach, so the push takes the direct route and
		// the tunnel has already done its job.
		logger.From(ctx).Debug("the zarf registry is reachable directly, not tunnelling the push", "registry", addr)
		return dst, nil
	}

	dialer, err := conn.To(addr)
	if err != nil {
		return mirrorTarget{}, err
	}
	logger.From(ctx).Info("tunnelling the mirror through a node", "host", conn.Address(), "registry", addr)

	dst.dial = dialer.DialContext
	dst.close = conn.Close
	handedOver = true
	return dst, nil
}

// zarfStateThrough reads the state secret over the tunnel.
func (o *packageMirrorOptions) zarfStateThrough(ctx context.Context, conn *tunnel.Conn) (zarfstate.Registry, apiDial, error) {
	endpoint, err := zarfstate.APIEndpoint(resolvedConfig.DistroOpts.KubeConfig, o.kubeContext)
	if err != nil {
		return zarfstate.Registry{}, nil, err
	}
	apiDialer, err := conn.To(endpoint)
	if err != nil {
		return zarfstate.Registry{}, nil, err
	}

	logger.From(ctx).Debug("reading zarf state through the tunnel", "host", conn.Address(), "api", endpoint)
	reg, err := zarfstate.Load(ctx, zarfstate.Options{
		Kubeconfig: resolvedConfig.DistroOpts.KubeConfig,
		Context:    o.kubeContext,
		Dial:       apiDialer.DialContext,
	})
	// The dialer is handed back because a zarf-managed-mTLS registry needs a second read from
	// the same API server for its CA, and opening a route of its own would defeat the point.
	return reg, apiDialer.DialContext, err
}

// firstControllerWithSSH picks the node the tunnel is opened through.
//
// Any controller will do -- a NodePort answers on every node, and a proxy-mode host port is
// served on each one -- so this takes the first rather than asking the operator to choose
// between equivalents. Only an `ssh:` host can carry a channel: rig's openssh protocol, which
// shells out to the ssh binary, implements no Dial, so a fleet configured that way is refused
// by name rather than failing later with something less obvious.
func firstControllerWithSSH(c clusterapi.Cluster) (*clusterapi.Host, error) {
	var sawController bool
	for _, h := range c.Spec.Hosts {
		if !h.IsController() {
			continue
		}
		sawController = true
		if h.ConnectionConfig.SSH != nil {
			return h, nil
		}
	}

	if sawController {
		return nil, fmt.Errorf("%w: no controller in the configuration has an `ssh:` block, and cargoship can only tunnel over its own ssh connection", tunnel.ErrNoSSH)
	}
	return nil, errors.New("the cluster configuration names no controller to tunnel through")
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
