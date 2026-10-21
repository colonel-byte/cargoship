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
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/colonel-byte.dev/v1alpha1/distro"
	"github.com/colonel-byte/cargoship/internal/clustercfg"
	"github.com/colonel-byte/cargoship/types/distrocfg"
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
	hosts             cluster.Hosts
	leader            *cluster.Host
	// nodes maps a host to the name its node carries in the cluster. See nodeName.
	nodes nodeNameCache
	// drift records why each host was selected. Prepare fills it while deciding which hosts to
	// sync, so Run can say what it is draining a node for without reading every file off that
	// host a second time. It is keyed by host pointer rather than by name: the same pointers
	// travel from the candidate list into p.hosts, while Host.Hostname is an optional
	// override that is empty on most hosts.
	driftMu sync.Mutex
	drift   map[*cluster.Host][]driftDetail
	// changed records whether this phase altered a host. It shares driftMu because it is
	// written from the same parallel sweep over the hosts that fills drift.
	changed bool
	// changedHosts records which hosts were altered, where changed records only that one was.
	// The two are kept apart rather than derived from each other because a caller reading the
	// fleet-wide answer should not have to care whether any phase bothered to track hosts.
	changedHosts map[*cluster.Host]bool
}

// driftDetail is one file on one host that differs from what the host should be carrying.
type driftDetail struct {
	// path is the file, absolute as the host sees it.
	path string
	// reason is why it is being rewritten, in the vocabulary the log lines already use.
	reason string
	// diff is a unified diff of the change, empty whenever there was nothing to compare: a
	// file that is missing, unreadable, stale, or differs only in its mode has no two versions
	// to put side by side.
	diff string
}

// String renders the detail the way the drift log line has always read: the file's base name
// and the reason in parentheses.
func (d driftDetail) String() string {
	return filepath.Base(d.path) + " (" + d.reason + ")"
}

// Drift reasons, named so that the decisions made on them are comparisons rather than string
// matching on a rendered label.
const (
	reasonMissing    = "missing"
	reasonUnreadable = "unreadable"
	reasonOutOfSync  = "out of sync"
	reasonWrongMode  = "wrong mode"
	reasonStale      = "stale"
)

// markChanged records that this phase altered the given hosts, and the fleet with them.
//
// It is called with the hosts a phase is about to act on rather than the ones it finished with:
// the first thing done to a host here is a drain, so a run that fails halfway has still changed
// the fleet and is expected to say so.
func (p *EngineConfigSyncHosts) markChanged(hosts ...*cluster.Host) {
	p.driftMu.Lock()
	defer p.driftMu.Unlock()
	p.markChangedLocked(hosts...)
}

// markChangedLocked is markChanged for a caller that already holds driftMu.
func (p *EngineConfigSyncHosts) markChangedLocked(hosts ...*cluster.Host) {
	p.changed = true
	if len(hosts) == 0 {
		return
	}
	if p.changedHosts == nil {
		p.changedHosts = map[*cluster.Host]bool{}
	}
	for _, h := range hosts {
		p.changedHosts[h] = true
	}
}

// HostsChanged reports what this phase did to each host it looked at. See hostChangedReporter in
// 05_manager.go.
//
// Every host Prepare checked is reported, including the ones that turned out to be in sync. A
// host that was checked and found correct is a different answer from a host nothing looked at,
// and only one of them means the fleet was fully assessed.
//
// The order is by address rather than by the order the hosts were reached, which is whatever the
// parallel sweep happened to finish in and would differ between two identical runs.
func (p *EngineConfigSyncHosts) HostsChanged() []HostChange {
	p.driftMu.Lock()
	defer p.driftMu.Unlock()

	out := make([]HostChange, 0, len(p.drift))
	for h, drifted := range p.drift {
		change := HostChange{
			Host:    h.ConfiguredAddress(),
			Changed: p.changedHosts[h],
		}
		for _, d := range drifted {
			change.Details = append(change.Details, HostChangeDetail{
				Path:   d.path,
				Reason: d.reason,
				Diff:   d.diff,
			})
		}
		out = append(out, change)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out
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

// nodeName is what the cluster calls h's node. See nodeNameCache.
func (p *EngineConfigSyncHosts) nodeName(ctx context.Context, h *cluster.Host) string {
	return p.nodes.nameFor(ctx, p.leader, p.Distro, p.hosts, h)
}

func (p *EngineConfigSyncHosts) prepareLeader(ctx context.Context) error {
	control := p.manager.Config.Spec.Hosts.Filter(func(h *cluster.Host) bool {
		return h.ServiceIsRunning(ctx, p.Distro.GetControllerService()) && h.IsController()
	})
	if len(control) == 0 {
		return ErrNoControllers
	}
	p.leader = control[0]
	return nil
}

func (p *EngineConfigSyncHosts) loadDesiredConfig(ctx context.Context, c *cluster.Cluster, dis distro.Distro) error {
	if err := clustercfg.DecryptRegistryAuth(c, p.Keyring); err != nil {
		return err
	}
	run := cluster.RuntimeMeta{Registries: c.Spec.Config.Registries}

	desired, err := p.Distro.DesiredFiles(ctx, &cluster.Host{}, run, dis)
	if err != nil {
		return err
	}
	p.desired = desired

	controllerDesired, err := p.Distro.DesiredFiles(ctx, &cluster.Host{Role: cluster.RoleController}, run, dis)
	if err != nil {
		return err
	}
	p.controllerDesired = controllerDesired
	return nil
}

// filesFor is the set of files a host is meant to carry. A controller carries what every host
// carries and then some, so an agent is never asked for a file that only a controller writes,
// and a controller is never told a manifest it does have is stale.
func (p *EngineConfigSyncHosts) filesFor(h *cluster.Host) map[string]distrocfg.DesiredFile {
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
func (p *EngineConfigSyncHosts) driftedFiles(h *cluster.Host) []driftDetail {
	var drifted []driftDetail
	for path, want := range p.filesFor(h) {
		if !h.FileExist(path) {
			drifted = append(drifted, driftDetail{path: path, reason: reasonMissing})
			continue
		}
		current, err := h.ReadFile(path)
		switch {
		case err != nil:
			drifted = append(drifted, driftDetail{path: path, reason: reasonUnreadable})
		case current != string(want.Content):
			// The host's copy and the desired one are both already in hand here, so the diff
			// costs no extra round trip. This is the only place in the phase that has both.
			drifted = append(drifted, driftDetail{
				path:   path,
				reason: reasonOutOfSync,
				diff:   unifiedDiff(path, current, string(want.Content)),
			})
		case !fileModeMatches(h, path, want.Mode):
			drifted = append(drifted, driftDetail{path: path, reason: reasonWrongMode})
		}
	}
	sort.Slice(drifted, func(i, j int) bool { return drifted[i].path < drifted[j].path })

	for _, path := range distrocfg.StaleFiles(h, p.Distro.ManagedDirs(), p.filesFor(h)) {
		drifted = append(drifted, driftDetail{path: path, reason: reasonStale})
	}
	return drifted
}

// driftLabels renders a drift list the way the log lines read. No drift renders as no list, the
// same way Titles answers for an empty phase list.
func driftLabels(drifted []driftDetail) []string {
	if len(drifted) == 0 {
		return nil
	}
	out := make([]string, 0, len(drifted))
	for _, d := range drifted {
		out = append(out, d.String())
	}
	return out
}

func (p *EngineConfigSyncHosts) needsUpdate(ctx context.Context, h *cluster.Host) bool {
	drifted := p.driftedFiles(h)

	p.driftMu.Lock()
	defer p.driftMu.Unlock()
	if p.drift == nil {
		p.drift = map[*cluster.Host][]driftDetail{}
	}
	p.drift[h] = drifted

	if len(drifted) == 0 {
		return false
	}

	// Decide whether any of the drift needs the engine restarted. A file the engine re-reads on
	// its own can be written in place; everything else means a drain and a rewrite.
	//
	// A stale file always counts: it is a file in a directory the distro owns that no longer
	// belongs there, and nothing re-reads its absence.
	desired := p.filesFor(h)
	restartRequired := false
	for _, d := range drifted {
		if d.reason == reasonStale {
			restartRequired = true
			break
		}
		if want, ok := desired[d.path]; ok && !want.NoRestart {
			restartRequired = true
			break
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
				"host", h, "drifted", strings.Join(driftLabels(drifted), ", "))
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
		// The host is marked here and not only in Run: these writes land during Prepare, and
		// the host is then not listed as needing a sync, so a run that changed only these
		// files would otherwise read as a run that touched nothing.
		p.markChangedLocked(h)
		// drifted is used rather than driftReason: this function holds driftMu, and
		// driftReason takes it for itself.
		logger.From(ctx).Info("updating files in place, the engine picks these up without a restart",
			"host", h, "files", written, "drifted", strings.Join(driftLabels(drifted), ", "))
		return false
	}

	return true
}

// driftReason reports what needsUpdate found on this host, for the log line that precedes a
// drain. It is empty when the host was never checked.
func (p *EngineConfigSyncHosts) driftReason(h *cluster.Host) string {
	p.driftMu.Lock()
	defer p.driftMu.Unlock()
	return strings.Join(driftLabels(p.drift[h]), ", ")
}

// fileModeMatches reports whether path on the host is already written with the mode want spells.
// Content is only half of what cargoship puts on a host -- a registries.yaml holding credentials
// is meant to be unreadable to anyone but root and the engine's group -- so a file someone has
// since widened is drift as much as a file with the wrong contents in it.
//
// A mode that cannot be parsed, or a file that cannot be stat'd, reports a match: there is
// nothing to compare against, and content comparison already decides whether the file is
// rewritten.
func fileModeMatches(h *cluster.Host, path, want string) bool {
	if want == "" {
		want = defaultFileMode
	}
	parsed, err := strconv.ParseUint(want, 8, 32)
	if err != nil {
		return true
	}
	info, err := h.Stat(path)
	if err != nil || info == nil {
		return true
	}
	return info.Mode().Perm() == fs.FileMode(parsed).Perm()
}

func (p *EngineConfigSyncHosts) writeFiles(_ context.Context, h *cluster.Host) error {
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

func (p *EngineConfigSyncHosts) drainNode(ctx context.Context, h *cluster.Host) error {
	logger.From(ctx).Info("draining node to sync engine config", "node", h, "drifted", p.driftReason(h))
	return p.drainClusterNode(ctx, p.leader, p.Distro, p.nodeName(ctx, h))
}

func (p *EngineConfigSyncHosts) startService(ctx context.Context, h *cluster.Host) error {
	logger.From(ctx).Info("waiting for the service to start", "service", p.service, "host", h)
	return p.startAndEnableService(ctx, h, p.service)
}

func (p *EngineConfigSyncHosts) waitForNodeReady(ctx context.Context, h *cluster.Host) error {
	logger.From(ctx).Info("waiting for the node to be in a ready state", "host", h)
	return p.waitNodeReady(ctx, p.leader, p.Distro, p.nodeName(ctx, h))
}

func (p *EngineConfigSyncHosts) uncordonNode(ctx context.Context, h *cluster.Host) error {
	return uncordonClusterNode(p.leader, p.Distro, p.nodeName(ctx, h))
}
