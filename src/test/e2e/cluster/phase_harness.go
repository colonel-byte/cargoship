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

package cluster

import (
	"context"
	"fmt"
	"os"
	"time"

	apicluster "github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/config"
	"github.com/colonel-byte/cargoship/src/internal/riglogger"
	"github.com/colonel-byte/cargoship/src/pkg/distro"
	"github.com/colonel-byte/cargoship/src/pkg/packager/load"
	"github.com/colonel-byte/cargoship/src/pkg/phase"
	"github.com/colonel-byte/cargoship/src/types/distrocfg"
	"github.com/colonel-byte/cargoship/src/types/distrocfg/registry"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// phaseHarness holds the in-process phase.Manager the apply phase tests drive. It is built
// the same way `cargoship apply` builds its manager -- the cluster inventory through
// load.ClusterDefinition, the distro through distro.Load -- so the phases under test see the
// state they would see during a real apply.
//
// The manager, and the hosts hanging off its config, are shared by every phase test: a phase
// records what it learned on the host it ran against (Configurer, Metadata, live SSH
// connection) and the next phase reads it back. That is what makes running one phase at a
// time possible, and it is also why the phase tests have to run in apply order.
type phaseHarness struct {
	manager *phase.Manager
	// distro is the distro module for the package under test, the same value action.NewApply
	// hands to every distro-aware phase.
	distro distrocfg.Distro
	// lock is retained so the unlock phase can be built from it, the way apply does.
	lock *phase.Lock
	// tempDir is the extracted distro package. The harness owns it and close removes it.
	tempDir string
	// dropped holds the upload-only hosts once dropUploadOnlyHosts has removed them from the
	// manager. They are kept so close can disconnect them: the Disconnect phase only sees the
	// hosts still on the manager.
	dropped apicluster.ZarfHosts
	// opts are the apply options the phase tests build their phases with.
	opts phaseHarnessOptions
}

// phaseHarnessOptions mirror the subset of action.ApplyOptions the phase tests exercise.
type phaseHarnessOptions struct {
	// Concurrency caps how many hosts a phase acts on at once. Zero means unlimited.
	Concurrency int
	// ModifyHosts enables the ModifyHosts phase.
	ModifyHosts bool
	// ModifyFirewall enables the ConfigureFirewall phase.
	ModifyFirewall bool
	// UpdateKubeConfig enables the KubeConfig phase.
	UpdateKubeConfig bool
	// LabelNodes enables the LabelNodes phase, which apply gates behind UpdateKubeConfig.
	LabelNodes bool
	// WorkerConcurrent is the worker batch size, a count ("5") or a percentage ("25%").
	WorkerConcurrent string
	// Timeout bounds the retry loops phases run through manager.RetryTimeout.
	Timeout time.Duration
}

// cachePath fills in the cache and temp-directory defaults the CLI's root command fills in
// before any command runs, and returns the absolute cache path the loaders take.
func cachePath() (string, error) {
	if config.CommonOptions.CachePath == "" {
		config.CommonOptions.CachePath = config.DefaultCachePath
	}
	if config.CommonOptions.TempDirectory == "" {
		config.CommonOptions.TempDirectory = os.TempDir()
	}
	return config.GetAbsCachePath()
}

// newManager builds the manager the install commands build, from the same two inputs: the
// inventory at configPath through load.ClusterDefinition and the package at pkgPath through
// distro.Load. It returns the distro module alongside it, the value the actions hand to
// every distro-aware phase.
//
// The manager owns the extracted package at manager.TempDirectory. The caller removes it,
// the way each command does on the way out.
func newManager(
	ctx context.Context,
	pkgPath, configPath string,
	concurrency int,
	timeout time.Duration,
) (*phase.Manager, distrocfg.Distro, error) {
	if err := riglogger.RigLogger(ctx); err != nil {
		return nil, nil, fmt.Errorf("failed to route rig logs: %w", err)
	}

	inventory, err := load.ClusterDefinition(ctx, configPath, load.ClusterOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load cluster inventory %s: %w", configPath, err)
	}

	cache, err := cachePath()
	if err != nil {
		return nil, nil, err
	}

	layout, err := distro.Load(ctx, pkgPath, distro.LoadOptions{
		CachePath:    cache,
		Architecture: config.CLIArch,
		Output:       config.CommonOptions.TempDirectory,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load distro package %s: %w", pkgPath, err)
	}

	dis, err := distroModule(layout.Distro.Spec.Type)
	if err != nil {
		return nil, nil, err
	}

	if concurrency < 0 {
		concurrency = 0
	}

	manager := &phase.Manager{
		Config:            &inventory,
		Distro:            &layout.Distro,
		DistroID:          layout.Distro.Spec.Type,
		TempDirectory:     layout.DirPath(),
		Concurrency:       concurrency,
		ConcurrentUploads: concurrency,
		Writer:            os.Stdout,
	}
	if timeout > 0 {
		manager.SetTimout(timeout)
	}

	return manager, dis, nil
}

// newBareManager builds the manager reset and kube-config build: an inventory and a distro
// ID, with no package loaded, because neither action installs anything. It has no temp
// directory, so there is nothing for the caller to clean up.
func newBareManager(ctx context.Context, configPath, distroID string, concurrency int) (*phase.Manager, error) {
	if err := riglogger.RigLogger(ctx); err != nil {
		return nil, fmt.Errorf("failed to route rig logs: %w", err)
	}

	inventory, err := load.ClusterDefinition(ctx, configPath, load.ClusterOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to load cluster inventory %s: %w", configPath, err)
	}

	if concurrency < 0 {
		concurrency = 0
	}

	return &phase.Manager{
		Config:            &inventory,
		DistroID:          distroID,
		Concurrency:       concurrency,
		ConcurrentUploads: concurrency,
		Writer:            os.Stdout,
	}, nil
}

// distroModule resolves a distro type to the module the phases are built with.
func distroModule(distroType string) (distrocfg.Distro, error) {
	builder, err := registry.GetDistroModuleBuilder(distroType)
	if err != nil {
		return nil, fmt.Errorf("no distro module for %q: %w", distroType, err)
	}
	dis, ok := builder().(distrocfg.Distro)
	if !ok {
		return nil, fmt.Errorf("distro module for %q does not implement distrocfg.Distro", distroType)
	}
	return dis, nil
}

// newPhaseHarness loads pkgPath and configPath into a manager the phase tests can step
// through one phase at a time.
func newPhaseHarness(ctx context.Context, pkgPath, configPath string, opts phaseHarnessOptions) (*phaseHarness, error) {
	manager, dis, err := newManager(ctx, pkgPath, configPath, opts.Concurrency, opts.Timeout)
	if err != nil {
		return nil, err
	}

	return &phaseHarness{
		manager: manager,
		distro:  dis,
		lock:    &phase.Lock{},
		tempDir: manager.TempDirectory,
		opts:    opts,
	}, nil
}

// run executes a single phase through the manager, so the phase still gets the
// SetManager/Prepare/ShouldRun/Before/Run/After treatment it gets inside a full apply.
func (h *phaseHarness) run(ctx context.Context, p phase.Phase) error {
	h.manager.SetPhases(phase.Phases{p})
	return h.manager.Run(ctx)
}

// ran reports whether the manager executed p rather than skipping it. Only conditional
// phases can be skipped, and their ShouldRun reads state their Prepare filled in, so this is
// only meaningful after run has returned.
func ran(p phase.Phase) bool {
	c, ok := p.(interface{ ShouldRun() bool })
	if !ok {
		return true
	}
	return c.ShouldRun()
}

// close releases the lock tickers the Lock phase started, disconnects any host the manager no
// longer holds, and removes the extracted package.
func (h *phaseHarness) close(ctx context.Context) error {
	if h.lock != nil {
		h.lock.Cancel(ctx)
	}
	for _, host := range h.dropped {
		host.Disconnect()
	}
	if h.tempDir == "" {
		return nil
	}
	return os.RemoveAll(h.tempDir)
}

// hosts returns every host in the inventory.
func (h *phaseHarness) hosts() apicluster.ZarfHosts {
	return h.manager.Config.Spec.Hosts
}

// controllers returns the hosts that carry a control-plane role.
func (h *phaseHarness) controllers() apicluster.ZarfHosts {
	return h.hosts().Filter(func(host *apicluster.ZarfHost) bool { return host.IsController() })
}

// workers returns the hosts that do not carry a control-plane role.
func (h *phaseHarness) workers() apicluster.ZarfHosts {
	return h.hosts().Filter(func(host *apicluster.ZarfHost) bool { return !host.IsController() })
}

// engineWorkers returns the workers that run the engine, so the upload-only hosts are left
// out. Before dropUploadOnlyHosts this differs from workers; after it, the two agree.
func (h *phaseHarness) engineWorkers() apicluster.ZarfHosts {
	return h.workers().Filter(func(host *apicluster.ZarfHost) bool { return !isUploadOnly(host) })
}

// engineHosts returns the hosts that run the engine, so every host minus the upload-only ones.
// Before dropUploadOnlyHosts this differs from hosts; after it, the two agree.
func (h *phaseHarness) engineHosts() apicluster.ZarfHosts {
	return h.hosts().Filter(func(host *apicluster.ZarfHost) bool { return !isUploadOnly(host) })
}

// uploadOnly returns the hosts that are in the inventory to receive uploads and nothing more.
func (h *phaseHarness) uploadOnly() apicluster.ZarfHosts {
	return h.hosts().Filter(isUploadOnly)
}

// dropUploadOnlyHosts removes the upload-only hosts from the manager, which is what stops the
// phases from the engine configuration onwards from acting on a host that cannot run the
// engine. It is called once, after the last upload phase, and returns what it removed.
//
// Removing them from the manager rather than teaching each test to skip them is deliberate:
// InitializeWorkers claims every non-controller host whose agent is not already running, with
// no OS gate, so a test that merely declined to assert on the Alpine node would still have
// watched the phase try to install rke2 on it.
func (h *phaseHarness) dropUploadOnlyHosts() apicluster.ZarfHosts {
	dropped := h.uploadOnly()
	if len(dropped) == 0 {
		return nil
	}
	h.manager.Config.Spec.Hosts = h.hosts().Filter(func(host *apicluster.ZarfHost) bool {
		return !isUploadOnly(host)
	})
	h.dropped = append(h.dropped, dropped...)
	return dropped
}

// lockFileContent is what the Lock phase writes into the lock file on every host: the
// hostname of the machine running the phases and the PID of this test binary.
func lockFileContent() (string, error) {
	hn, err := os.Hostname()
	if err != nil {
		hn = "unknown"
	}
	return fmt.Sprintf("%s-%d", hn, os.Getpid()), nil
}

// carriesFilesFor reports whether the package under test ships any OS file for the given
// package-manager selector (config.SelectorRPM, SelectorAPT, SelectorBIN). The upload phases
// route on it, and a package that carries nothing for a selector makes its phase a no-op even
// on hosts of the matching family, so a test has to know which of the two it is looking at.
func (h *phaseHarness) carriesFilesFor(selector string) bool {
	for _, f := range h.manager.Distro.Spec.Config.OS.Files {
		if f.Selector.Package == selector {
			return true
		}
	}
	return false
}

// readOnHosts reads path from every host, keyed by host string, so a test can assert on the
// same file across the whole cluster.
func readOnHosts(hosts apicluster.ZarfHosts, path string) (map[string]string, error) {
	out := make(map[string]string, len(hosts))
	for _, host := range hosts {
		content, err := host.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s: failed to read %s: %w", host, path, err)
		}
		out[host.String()] = content
	}
	return out, nil
}

// phaseCtx builds a context carrying a debug logger, matching what the CLI hands the phases.
func phaseCtx(ctx context.Context) (context.Context, error) {
	l, err := logger.New(logger.Config{
		Level:       logger.Debug,
		Format:      logger.FormatConsole,
		Destination: logger.DestinationDefault,
		Color:       false,
	})
	if err != nil {
		return nil, err
	}
	return logger.WithContext(ctx, l), nil
}
