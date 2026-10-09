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

package tofuprovider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/config"
	"github.com/colonel-byte/cargoship/internal/riglogger"
	"github.com/colonel-byte/cargoship/pkg/action"
	"github.com/colonel-byte/cargoship/pkg/distro"
	"github.com/colonel-byte/cargoship/pkg/helmvalues"
	"github.com/colonel-byte/cargoship/pkg/phase"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// hostFacts is what the read-only phases learned about one host.
//
// EngineVersion is the attribute the plan-time downgrade check of #307 compares against, which is
// why it is in the schema before there is anything to compare: an attribute that does not exist
// cannot be compared later. See docs/agent/choice-tofu-provider-layout.md.
type hostFacts struct {
	Address        string
	Hostname       string
	OS             string
	OSVersion      string
	Arch           string
	PrivateAddress string
	Role           string
	EngineVersion  string
}

// applyOptions are the apply settings a resource exposes. They are the subset of
// action.ApplyOptions that changes what an apply does to a fleet rather than what it reports.
type applyOptions struct {
	// Package is the distro package to install: a path, or an OCI reference.
	Package string
	// ValuesFiles are file paths to YAML values files overriding the values the package ships with.
	ValuesFiles []string
	// ModifyHosts rewrites /etc/hosts on every host.
	ModifyHosts bool
	// ModifyFirewall rewrites the host firewall.
	ModifyFirewall bool
	// LabelNodes adds the node-role.kubernetes.io/<profile> label to each node.
	LabelNodes bool
	// WorkerConcurrent is the worker batch size, a count ("5") or a percentage ("25%").
	WorkerConcurrent string
	// AllowUnmanagedNodes continues when the cluster holds a node the configuration does not.
	AllowUnmanagedNodes bool
	// AllowDowngrade continues when a host runs a version newer than the package.
	AllowDowngrade bool
	// Timeout bounds the retry loops the phases run through.
	Timeout time.Duration
	// Kubeconfig asks for the cluster's admin credentials to be returned. Nothing is written to
	// the machine running OpenTofu either way -- see rule 4 of docs/agent/choice-tofu-secrets.md
	// for why they are not returned unless they are asked for.
	Kubeconfig bool
}

// applyResult is what an apply leaves for the resource to write to state.
type applyResult struct {
	// DistroID is the engine the package carries, which is what a later refresh needs and what a
	// configuration never states twice.
	DistroID string
	// EngineVersion is the version the package carries.
	EngineVersion string
	// Facts are the per-host facts the read-only phases gathered on the way through.
	Facts []hostFacts
	// Kubeconfig is the cluster's admin credentials, populated only when they were asked for.
	Kubeconfig []byte
}

// teardownOptions are the reset settings a resource exposes.
type teardownOptions struct {
	// DistroID is the engine to remove. A reset loads no package, so it has nothing else to read
	// the engine's identity from.
	DistroID string
	// WorkerConcurrent is the batch size nodes are drained and deleted in.
	WorkerConcurrent string
	// NoDrain skips draining a node before deleting it.
	NoDrain bool
	// Timeout bounds the retry loops the phases run through.
	Timeout time.Duration
}

// converger is everything the provider asks of cargoship.
//
// It exists as an interface so the resources and data sources can be tested without SSH, a
// cluster, or a distro package: the real implementation builds a phase.Manager and runs an action,
// and a fake one answers from a table. Provider bugs live in state mapping and diagnostics rather
// than in the phases, and those are only testable if this seam exists.
type converger interface {
	// Refresh runs the read-only phases and reports what they found, host by host. It changes
	// nothing, which is what lets a data source and a resource's Read share it.
	Refresh(ctx context.Context, c *cluster.ZarfCluster, distroID string) ([]hostFacts, error)
	// DistroFromPackage reads the engine the package carries, for the paths that have no engine
	// recorded in state to act on.
	DistroFromPackage(ctx context.Context, pkg string) (string, error)
	// Apply converges the cluster on the configuration: one phase list covering install, join
	// and upgrade, each phase gated by its own ShouldRun, which is why Create and Update are the
	// same call.
	//
	// A result comes back even when the error is non-nil, and it is not empty: an apply that
	// failed part way through has already changed hosts, and the facts it gathered before failing
	// are what stop the next plan from deciding nothing is installed.
	Apply(ctx context.Context, c *cluster.ZarfCluster, opts applyOptions) (applyResult, error)
	// Teardown removes the engine and its data from every host in the configuration.
	Teardown(ctx context.Context, c *cluster.ZarfCluster, opts teardownOptions) error
}

// DefaultConnectTimeout bounds how long a read waits for a fleet.
//
// It exists because the connect phase is built for the CLI, where a host rebooting into a new
// kernel is ordinary: it retries for ten minutes before giving up (pkg/phase/07_connect.go). That
// is the right answer for an apply somebody is watching and the wrong one for a plan, which would
// sit silent for ten minutes on a typo in an address. A plan that fails in a minute with the
// address in the message is more useful than one that eventually fails with the same message.
const DefaultConnectTimeout = time.Minute

// cargoshipConverger is the real implementation: an in-process phase.Manager, built the way the
// CLI builds one.
type cargoshipConverger struct {
	// concurrency caps how many hosts a phase acts on at once. Zero means unlimited.
	concurrency int
	// connectTimeout bounds a read, DefaultConnectTimeout when zero.
	connectTimeout time.Duration
	// out is where a phase's own progress writing goes. A provider has no terminal, so the
	// default is io.Discard and the log lines reach tofu through the logger instead.
	out io.Writer
}

// Refresh runs action.NewRefresh, which is the read-only half of an apply: it connects, resolves
// each host's OS, gathers the network facts and reads the engine version already installed, and
// disconnects. Every phase in it declares ReadOnly() and no cluster lock is taken, which is what
// makes it safe during a plan -- and it lives in pkg/action rather than here so that "which phases
// are safe to run" is decided in one place rather than two.
func (c cargoshipConverger) Refresh(ctx context.Context, cfg *cluster.ZarfCluster, distroID string) ([]hostFacts, error) {
	ctx = withTofuLogger(ctx)
	if distroID == "" {
		return nil, fmt.Errorf("distro is required: the engine to read, %q or %q", "k3s", "rke2")
	}

	refresh, err := action.NewRefresh(action.RefreshOptions{
		Manager: c.manager(cfg, distroID),
	})
	if err != nil {
		return nil, err
	}

	// rig logs through its own logger; routing it onto the context's logger is what puts an SSH
	// failure in the provider's diagnostics rather than nowhere.
	if err := riglogger.RigLogger(ctx); err != nil {
		return nil, fmt.Errorf("unable to route the SSH logs: %w", err)
	}

	timeout := c.connectTimeout
	if timeout <= 0 {
		timeout = DefaultConnectTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := refresh.Run(ctx); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("gave up after %s: %w (raise connect_timeout if the fleet is slow to answer)", timeout, err)
		}
		return nil, err
	}
	return factsOf(cfg.Spec.Hosts), nil
}

// factsOf reads the facts the phases left on the hosts.
//
// The phases record what they learn on the host objects themselves, so this is read after the run
// rather than collected during it. A host that was never reached carries zero values, which is the
// honest answer: the run failed, and the caller is about to report that.
//
// The OS release is read through host.OS() rather than off the metadata, because that is where
// DetectOS caches it -- and it is read only when the host has one, since the accessor would
// otherwise try to detect it over a connection the disconnect phase has already closed.
func factsOf(hosts cluster.ZarfHosts) []hostFacts {
	facts := make([]hostFacts, 0, len(hosts))
	for _, host := range hosts {
		fact := hostFacts{
			Address:        addressOf(host),
			Hostname:       host.Metadata.Hostname,
			Arch:           host.Metadata.Arch,
			PrivateAddress: host.PrivateAddress,
			Role:           host.Role,
			EngineVersion:  host.Metadata.DistroVersion,
		}
		if host.OSRelease != nil {
			fact.OS = host.OSRelease.ID
			fact.OSVersion = host.OSRelease.Version
		}
		facts = append(facts, fact)
	}
	return facts
}

// addressOf is the address a configuration would recognise: what the host block said, with the
// port when it is not the default.
//
// rig's CompositeConfig.String renders a Go-ish "ssh.Config{127.0.0.1:2223}", which is right for a
// log line and wrong for an attribute somebody writes a comparison against. ZarfHost.Address is
// not an option either: it reads through the embedded rig client, which a host that has already
// been disconnected no longer has.
func addressOf(host *cluster.ZarfHost) string {
	cfg := host.ConnectionConfig.SSH
	if cfg == nil {
		return host.ConnectionConfig.String()
	}
	if cfg.Port == 0 || cfg.Port == defaultSSHPort {
		return cfg.Address
	}
	return net.JoinHostPort(cfg.Address, strconv.Itoa(cfg.Port))
}

// DistroFromPackage loads the package's definition and returns the engine type it carries.
func (c cargoshipConverger) DistroFromPackage(ctx context.Context, pkg string) (string, error) {
	if pkg == "" {
		return "", errors.New("package is required: the distro package to inspect")
	}

	cache, err := cachePath()
	if err != nil {
		return "", err
	}
	layout, err := distro.Load(ctx, pkg, distro.LoadOptions{
		CachePath:    cache,
		Architecture: config.CLIArch,
		Output:       config.CommonOptions.TempDirectory,
	})
	if err != nil {
		return "", fmt.Errorf("unable to load the package %s: %w", pkg, err)
	}
	defer func() {
		if err := os.RemoveAll(layout.DirPath()); err != nil {
			tflog.Warn(ctx, "could not remove the extracted package", map[string]any{
				"path":  layout.DirPath(),
				"error": err.Error(),
			})
		}
	}()

	return layout.Distro.Spec.Type, nil
}

// Apply loads the package and converges the cluster on it.
//
// The package is loaded here rather than in the resource because what comes out of it -- the
// engine's identity and its version -- is what the manager needs and what the resource writes to
// state. A configuration states the package once; it never states the engine as well.
//
// The extracted package is removed on the way out. It is tens of megabytes of staging data per
// apply, and nothing after the run reads it.
func (c cargoshipConverger) Apply(ctx context.Context, cfg *cluster.ZarfCluster, opts applyOptions) (applyResult, error) {
	ctx = withTofuLogger(ctx)
	var result applyResult

	if opts.Package == "" {
		return result, fmt.Errorf("package is required: the distro package to install")
	}

	cache, err := cachePath()
	if err != nil {
		return result, err
	}
	layout, err := distro.Load(ctx, opts.Package, distro.LoadOptions{
		CachePath:    cache,
		Architecture: config.CLIArch,
		Output:       config.CommonOptions.TempDirectory,
	})
	if err != nil {
		return result, fmt.Errorf("unable to load the package %s: %w", opts.Package, err)
	}
	defer func() {
		if err := os.RemoveAll(layout.DirPath()); err != nil {
			tflog.Warn(ctx, "could not remove the extracted package", map[string]any{
				"path":  layout.DirPath(),
				"error": err.Error(),
			})
		}
	}()

	result.DistroID = layout.Distro.Spec.Type
	result.EngineVersion = layout.Distro.Spec.Version

	overrides := []map[string]any{cfg.Spec.Config.Values}
	if len(opts.ValuesFiles) > 0 {
		fromFiles, err := helmvalues.LoadFiles(ctx, "", "", opts.ValuesFiles)
		if err != nil {
			return result, fmt.Errorf("unable to read values files: %w", err)
		}
		overrides = append(overrides, fromFiles)
	}

	values, err := layout.Values(ctx, overrides...)
	if err != nil {
		return result, err
	}
	if err := layout.ApplyValues(values); err != nil {
		return result, err
	}
	if err := layout.RenderFiles(ctx, values); err != nil {
		return result, err
	}

	manager := c.manager(cfg, layout.Distro.Spec.Type)
	manager.Distro = &layout.Distro
	manager.Values = values
	manager.TempDirectory = layout.DirPath()
	if opts.Timeout > 0 {
		manager.SetTimeout(opts.Timeout)
	}

	apply, err := action.NewApply(action.ApplyOptions{
		Manager:             manager,
		ModifyHosts:         opts.ModifyHosts,
		ModifyFirewall:      opts.ModifyFirewall,
		LabelNodes:          opts.LabelNodes,
		WorkerConcurrent:    opts.WorkerConcurrent,
		AllowUnmanagedNodes: opts.AllowUnmanagedNodes,
		AllowDowngrade:      opts.AllowDowngrade,
		// The apply never writes the operator's kubeconfig. A provider that merged credentials
		// into ~/.kube/config as a side effect of an apply would be changing the machine running
		// OpenTofu, which is not what the resource describes. The credentials are read back
		// afterwards instead, and only when they were asked for.
		UpdateKubeConfig: opts.Kubeconfig,
		NoKubeConfigFile: true,
	})
	if err != nil {
		return result, err
	}

	if err := riglogger.RigLogger(ctx); err != nil {
		return result, fmt.Errorf("unable to route the SSH logs: %w", err)
	}

	runErr := apply.Run(ctx)

	// The facts come back either way. An apply that failed has still changed hosts, and the next
	// plan reading an empty state would decide nothing is installed and re-bootstrap a live
	// cluster.
	result.Facts = factsOf(cfg.Spec.Hosts)

	if opts.Kubeconfig && runErr == nil {
		bytes, err := apply.KubeConfigBytes()
		if err != nil {
			return result, fmt.Errorf("the apply finished but the kubeconfig could not be read: %w", err)
		}
		result.Kubeconfig = bytes
	}
	return result, runErr
}

// Teardown removes the engine from every host, which is what a destroy does.
func (c cargoshipConverger) Teardown(ctx context.Context, cfg *cluster.ZarfCluster, opts teardownOptions) error {
	ctx = withTofuLogger(ctx)
	if opts.DistroID == "" {
		return fmt.Errorf("the engine to remove is not recorded in state, so there is nothing to tear down safely")
	}

	manager := c.manager(cfg, opts.DistroID)
	if opts.Timeout > 0 {
		manager.SetTimeout(opts.Timeout)
	}

	reset, err := action.NewReset(action.ResetOptions{
		Manager:          manager,
		WorkerConcurrent: opts.WorkerConcurrent,
		NoDrain:          opts.NoDrain,
		NoWait:           true,
	})
	if err != nil {
		return err
	}

	if err := riglogger.RigLogger(ctx); err != nil {
		return fmt.Errorf("unable to route the SSH logs: %w", err)
	}
	return reset.Run(ctx)
}

// manager builds the phase.Manager every action here runs through, the way the CLI builds one.
func (c cargoshipConverger) manager(cfg *cluster.ZarfCluster, distroID string) *phase.Manager {
	out := c.out
	if out == nil {
		out = io.Discard
	}
	return &phase.Manager{
		Config:            cfg,
		DistroID:          distroID,
		Concurrency:       c.concurrency,
		ConcurrentUploads: c.concurrency,
		Writer:            out,
	}
}

// cachePath fills in the cache and temp-directory defaults the CLI's root command fills in before
// any command runs. A provider has no root command, so it does it here.
func cachePath() (string, error) {
	if config.CommonOptions.CachePath == "" {
		config.CommonOptions.CachePath = config.DefaultCachePath
	}
	if config.CommonOptions.TempDirectory == "" {
		config.CommonOptions.TempDirectory = os.TempDir()
	}
	if config.CLIArch == "" {
		config.CLIArch = runtime.GOARCH
	}
	return config.GetAbsCachePath()
}

// withTofuLogger bridges cargoship's context-scoped logger to tflog.
func withTofuLogger(ctx context.Context) context.Context {
	return logger.WithContext(ctx, slog.New(tofuSlogHandler{attrs: make(map[string]any)}))
}

type tofuSlogHandler struct {
	attrs map[string]any
}

func (tofuSlogHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h tofuSlogHandler) Handle(ctx context.Context, r slog.Record) error {
	fields := make(map[string]any, len(h.attrs)+r.NumAttrs())
	for k, v := range h.attrs {
		fields[k] = v
	}
	r.Attrs(func(a slog.Attr) bool {
		appendSlogAttr(fields, "", a)
		return true
	})

	switch {
	case r.Level >= slog.LevelError:
		tflog.Error(ctx, r.Message, fields)
	case r.Level >= slog.LevelWarn:
		tflog.Warn(ctx, r.Message, fields)
	case r.Level >= slog.LevelInfo:
		tflog.Info(ctx, r.Message, fields)
	default:
		tflog.Debug(ctx, r.Message, fields)
	}
	return nil
}

func appendSlogAttr(fields map[string]any, prefix string, a slog.Attr) {
	key := a.Key
	if prefix != "" {
		key = prefix + "." + key
	}
	val := a.Value.Resolve()
	if val.Kind() == slog.KindGroup {
		for _, child := range val.Group() {
			appendSlogAttr(fields, key, child)
		}
		return
	}
	fields[key] = val.Any()
}

func (h tofuSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make(map[string]any, len(h.attrs)+len(attrs))
	for k, v := range h.attrs {
		next[k] = v
	}
	for _, a := range attrs {
		appendSlogAttr(next, "", a)
	}
	return tofuSlogHandler{attrs: next}
}

func (h tofuSlogHandler) WithGroup(_ string) slog.Handler {
	return h
}
