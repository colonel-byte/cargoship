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

package phase

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/colonel-byte/cargoship/internal/clustercfg"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/src/pkg/node"
	"github.com/colonel-byte/cargoship/src/types/distrocfg"
	"github.com/k0sproject/rig/exec"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// defaultFileMode is what an engine config file is written with when the distro did not say.
// Root-only is the safe answer: these files can carry registry credentials.
const defaultFileMode = "0600"

// EngineConfigSyncHosts phase state
type EngineConfigSyncHosts struct {
	GenericPhase
	Distro distrocfg.Distro
	// Keyring decrypts encrypted registry credentials, in either supported format.
	Keyring *clustercfg.Keyring
	// desired is what every host carries, and controllerDesired is that plus the files only a
	// controller gets -- the HelmChartConfig manifests the engine's helm controller reads.
	// Both are built once, since neither depends on the host beyond its role.
	desired           map[string]distrocfg.DesiredFile
	controllerDesired map[string]distrocfg.DesiredFile
	service           string
	hosts             cluster.ZarfHosts
	leader            *cluster.ZarfHost
	// drift records why each host was selected. Prepare fills it while deciding which hosts to
	// sync, so Run can say what it is draining a node for without reading every file off that
	// host a second time. It is keyed by host pointer rather than by name: the same pointers
	// travel from the candidate list into p.hosts, while ZarfHost.Hostname is an optional
	// override that is empty on most hosts.
	driftMu sync.Mutex
	drift   map[*cluster.ZarfHost][]string
	// changed records whether this phase altered a host. It shares driftMu because it is
	// written from the same parallel sweep over the hosts that fills drift.
	changed bool
}

// markChanged records that this phase altered a host.
func (p *EngineConfigSyncHosts) markChanged() {
	p.driftMu.Lock()
	defer p.driftMu.Unlock()
	p.changed = true
}

// Changed reports whether this phase altered any host, which is what Ansible's changed is built
// from. See changedReporter in 05_manager.go.
//
// There are two ways this phase changes a host and both count. The obvious one is the sync
// itself. The other is the file a host carries that the engine re-reads without a restart: that
// is written while Prepare is deciding which hosts have drifted, and the host is then not listed
// as needing a sync, so a run that changed only those files would otherwise read as a run that
// did nothing.
func (p *EngineConfigSyncHosts) Changed() bool {
	p.driftMu.Lock()
	defer p.driftMu.Unlock()
	return p.changed
}

// ShouldRun is true when there are hosts to sync
func (p *EngineConfigSyncHosts) ShouldRun() bool {
	return len(p.hosts) > 0
}

func (p *EngineConfigSyncHosts) prepareLeader() error {
	control := p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return h.Configurer.ServiceIsRunning(h, p.Distro.GetControllerService()) && h.IsController()
	})
	if len(control) == 0 {
		return ErrNoControllers
	}
	p.leader = control[0]
	return nil
}

func (p *EngineConfigSyncHosts) loadDesiredConfig(c *cluster.ZarfCluster, dis distro.ZarfDistro) error {
	if err := clustercfg.DecryptRegistryAuth(c, p.Keyring); err != nil {
		return err
	}
	run := cluster.ZarfRuntimeMeta{Registries: c.Spec.Config.Registries}

	desired, err := p.Distro.DesiredFiles(cluster.ZarfHost{}, run, dis)
	if err != nil {
		return err
	}
	p.desired = desired

	controllerDesired, err := p.Distro.DesiredFiles(cluster.ZarfHost{Role: cluster.RoleController}, run, dis)
	if err != nil {
		return err
	}
	p.controllerDesired = controllerDesired
	return nil
}

// filesFor is the set of files a host is meant to carry. A controller carries what every host
// carries and then some, so an agent is never asked for a file that only a controller writes,
// and a controller is never told a manifest it does have is stale.
func (p *EngineConfigSyncHosts) filesFor(h *cluster.ZarfHost) map[string]distrocfg.DesiredFile {
	if h.IsController() && p.controllerDesired != nil {
		return p.controllerDesired
	}
	return p.desired
}

// driftedFiles names the desired files the host does not already have, each with the reason it
// is being rewritten, followed by any file left over in a directory the distro owns. An
// unreadable file is reported as such rather than as a content difference, since the two call
// for different things from whoever reads the log. The list is sorted so that the same drift
// reads the same way from one run to the next.
func (p *EngineConfigSyncHosts) driftedFiles(h *cluster.ZarfHost) []string {
	var drifted []string
	for path, want := range p.filesFor(h) {
		base := filepath.Base(path)
		if !h.FileExist(path) {
			drifted = append(drifted, base+" (missing)")
			continue
		}
		current, err := h.ReadFile(path)
		switch {
		case err != nil:
			drifted = append(drifted, base+" (unreadable)")
		case current != string(want.Content):
			drifted = append(drifted, base+" (out of sync)")
		case !fileModeMatches(h, path, want.Mode):
			drifted = append(drifted, base+" (wrong mode)")
		}
	}
	sort.Strings(drifted)

	for _, path := range distrocfg.StaleFiles(h, p.Distro.ManagedDirs(), p.filesFor(h)) {
		drifted = append(drifted, filepath.Base(path)+" (stale)")
	}
	return drifted
}

func (p *EngineConfigSyncHosts) needsUpdate(ctx context.Context, h *cluster.ZarfHost) bool {
	drifted := p.driftedFiles(h)

	p.driftMu.Lock()
	defer p.driftMu.Unlock()
	if p.drift == nil {
		p.drift = map[*cluster.ZarfHost][]string{}
	}
	p.drift[h] = drifted

	if len(drifted) == 0 {
		return false
	}

	// Check if all drifted files are flagged NoRestart. If so, write them directly without triggering node drain/restart.
	restartRequired := false
	for path, want := range p.filesFor(h) {
		if !want.NoRestart {
			base := filepath.Base(path)
			for _, d := range drifted {
				if strings.HasPrefix(d, base+" ") {
					restartRequired = true
					break
				}
			}
		}
		if restartRequired {
			break
		}
	}

	// Also check if any stale file requires engine restart (stale files in managed dirs)
	if !restartRequired {
		for _, d := range drifted {
			if strings.HasSuffix(d, "(stale)") {
				restartRequired = true
				break
			}
		}
	}

	if !restartRequired {
		// All drift is in NoRestart files -- a HelmChartConfig the engine re-reads on its
		// own, say, or distro-release.json. Write them in place immediately.
		//
		// Except under a dry run. Prepare runs for every phase, including the ones a dry run
		// is about to skip, which is how a dry run can report what a phase would do. Writing
		// here would make it the one place a dry run touched a host.
		if p.manager != nil && p.manager.DryRun {
			logger.From(ctx).Info("would update files in place, the engine picks these up without a restart",
				"host", h, "drifted", strings.Join(drifted, ", "))
			return false
		}
		// A write that fails leaves the file as drifted as it was found, so the host is reported
		// as needing an update and picks the file up on the drain-and-rewrite path rather than
		// being counted as synced by a write that did not land.
		var written []string
		for path, file := range p.filesFor(h) {
			if !file.NoRestart {
				continue
			}
			mode := file.Mode
			if mode == "" {
				mode = defaultFileMode
			}
			if err := h.WriteFile(path, string(file.Content), mode); err != nil {
				return true
			}
			written = append(written, path)
		}
		// The host never appears in the list of hosts this phase acts on, so without
		// this the run reads as though nothing happened -- which is what a values change
		// that lands entirely in manifests would otherwise look like.
		sort.Strings(written)
		p.changed = true
		// drifted is used rather than driftReason: this function holds driftMu, and
		// driftReason takes it for itself.
		logger.From(ctx).Info("updating files in place, the engine picks these up without a restart",
			"host", h, "files", written, "drifted", strings.Join(drifted, ", "))
		return false
	}

	return true
}

// driftReason reports what needsUpdate found on this host, for the log line that precedes a
// drain. It is empty when the host was never checked.
func (p *EngineConfigSyncHosts) driftReason(h *cluster.ZarfHost) string {
	p.driftMu.Lock()
	defer p.driftMu.Unlock()
	return strings.Join(p.drift[h], ", ")
}

// fileModeMatches reports whether path on the host is already written with the mode want spells.
// Content is only half of what cargoship puts on a host -- a registries.yaml holding credentials
// is meant to be unreadable to anyone but root and the engine's group -- so a file someone has
// since widened is drift as much as a file with the wrong contents in it.
//
// A mode that cannot be parsed, or a file that cannot be stat'd, reports a match: there is
// nothing to compare against, and content comparison already decides whether the file is
// rewritten.
func fileModeMatches(h *cluster.ZarfHost, path, want string) bool {
	if want == "" {
		want = defaultFileMode
	}
	parsed, err := strconv.ParseUint(want, 8, 32)
	if err != nil {
		return true
	}
	info, err := h.Stat(path, exec.Sudo(h))
	if err != nil || info == nil {
		return true
	}
	return info.Mode().Perm() == fs.FileMode(parsed).Perm()
}

func (p *EngineConfigSyncHosts) writeFiles(_ context.Context, h *cluster.ZarfHost) error {
	for path, file := range p.filesFor(h) {
		mode := file.Mode
		if mode == "" {
			mode = defaultFileMode
		}
		if err := h.WriteFile(path, string(file.Content), mode); err != nil {
			return err
		}
	}
	// The node is already stopped and about to be restarted, which is the one moment a file the
	// configuration no longer calls for can be removed without the engine noticing it go.
	return distrocfg.RemoveStaleFiles(h, p.Distro.ManagedDirs(), p.filesFor(h))
}

func (p *EngineConfigSyncHosts) drainNode(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("draining node to sync engine config", "node", h, "drifted", p.driftReason(h))
	return p.manager.RetryTimeout(ctx, func(_ context.Context) error {
		return p.leader.Exec(p.Distro.KubectlCmdf(*p.leader, p.Distro.DataDirPath(), drainNode, h.Configurer.Hostname(h)), exec.Sudo(p.leader))
	})
}

func (p *EngineConfigSyncHosts) startService(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("waiting for the service to start", "service", p.service, "host", h)

	startedAt := time.Now()
	go func() {
		err := h.Configurer.StartService(h, p.service)
		if err != nil {
			logger.From(ctx).Warn("failed to start", "service", p.service, "host", h)
		}
	}()

	if err := p.manager.RetryTimeout(ctx, node.ServiceRunningFunc(h, p.service)); err != nil {
		return p.captureServiceLogsOnFailure(ctx, h, p.service, startedAt, err)
	}

	return h.Configurer.EnableService(h, p.service)
}

func (p *EngineConfigSyncHosts) waitForNodeReady(ctx context.Context, h *cluster.ZarfHost) error {
	logger.From(ctx).Info("waiting for the node to be in a ready state", "host", h)

	return p.manager.RetryTimeout(ctx, func(_ context.Context) error {
		out, err := p.leader.ExecOutput(p.Distro.KubectlCmdf(*p.leader, p.Distro.DataDirPath(), readyNode, h.Configurer.Hostname(h)), exec.Sudo(p.leader))
		if err != nil {
			return err
		}
		if strings.ToLower(out) != "true" {
			return errors.New("node not ready")
		}
		return nil
	})
}

func (p *EngineConfigSyncHosts) uncordonNode(_ context.Context, h *cluster.ZarfHost) error {
	return p.leader.Exec(p.Distro.KubectlCmdf(*p.leader, p.Distro.DataDirPath(), uncordonNode, h.Configurer.Hostname(h)), exec.Sudo(p.leader))
}
