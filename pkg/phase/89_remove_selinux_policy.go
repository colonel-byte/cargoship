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
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/cluster"
	"github.com/colonel-byte/cargoship/api/zarf.dev/v1alpha1/distro"
	"github.com/zarf-dev/zarf/src/pkg/logger"
)

// RemoveSelinuxPolicy takes the SELinux policy cargoship applied back off the hosts.
type RemoveSelinuxPolicy struct {
	GenericPhase
	selinuxPolicyHosts cluster.ZarfHosts
}

// Prepare the phase
func (p *RemoveSelinuxPolicy) Prepare(ctx context.Context, _ *cluster.ZarfCluster, _ *distro.ZarfDistro) error {
	p.selinuxPolicyHosts = p.manager.Config.Spec.Hosts.Filter(func(h *cluster.ZarfHost) bool {
		return selinuxPolicyCapable(h) && h.FileExist(SELinuxStateFile)
	})

	logger.From(ctx).Info("number of systems with an selinux policy to remove", "hosts", len(p.selinuxPolicyHosts))

	return nil
}

// Title for the phase
func (p *RemoveSelinuxPolicy) Title() string {
	return "Remove the SELinux policy cargoship applied"
}

// Explanation about the current phase, used for documentation generation
func (p *RemoveSelinuxPolicy) Explanation() string {
	return "Removes the policy modules and file contexts recorded in " + SELinuxStateFile + " and returns the booleans to the values the host held before cargoship changed them"
}

// Run the phase
func (p *RemoveSelinuxPolicy) Run(ctx context.Context) error {
	return p.parallelDoWithMessage(
		ctx,
		"removing selinux policy",
		p.selinuxPolicyHosts,
		p.removeHost,
	)
}

// ShouldRun is true when a host carries a policy cargoship recorded
func (p *RemoveSelinuxPolicy) ShouldRun() bool {
	return len(p.selinuxPolicyHosts) > 0
}

// removeHost unwinds one host in the reverse of the order the policy went on: contexts, then
// booleans, then the modules the contexts may have referred to.
//
// Every step warns and carries on rather than returning. A reset has to be re-runnable, and the
// state it is undoing can legitimately be half gone - a module removed by hand, a context already
// deleted - so a failure to remove something that is not there must not strand the host with the
// state file still claiming it is.
func (p *RemoveSelinuxPolicy) removeHost(ctx context.Context, h *cluster.ZarfHost) error {
	state, err := readSELinuxState(h)
	if err != nil {
		logger.From(ctx).Warn("could not read the selinux state file, leaving the policy in place", "host", h, "file", SELinuxStateFile, "error", err)
		return nil
	}

	p.removeFileContexts(ctx, h, state)
	p.restoreBooleans(ctx, h, state)
	p.removeModules(ctx, h, state)

	if err := p.Wet(h, "delete "+SELinuxStateFile, func() error {
		return h.DeleteFile(SELinuxStateFile)
	}); err != nil {
		logger.From(ctx).Warn("could not delete the selinux state file", "host", h, "file", SELinuxStateFile, "error", err)
	}

	return nil
}

// removeFileContexts deletes every recorded mapping and relabels the paths back to the default.
func (p *RemoveSelinuxPolicy) removeFileContexts(ctx context.Context, h *cluster.ZarfHost, state selinuxState) {
	for _, fc := range state.FileContexts {
		del := selinuxFcontextCommand("-d", distro.ZarfDistroSELinuxFileContext{
			Path:     fc.Path,
			Type:     fc.Type,
			FileType: fc.FileType,
		})
		logger.From(ctx).Info("removing selinux file context", "host", h, "path", fc.Path)
		if err := p.Wet(h, del, func() error {
			return h.SudoExec(del)
		}); err != nil {
			logger.From(ctx).Warn("could not remove selinux file context", "host", h, "path", fc.Path, "error", err)
			continue
		}

		if err := p.relabelSELinux(ctx, h, fc.Path); err != nil {
			logger.From(ctx).Warn("could not relabel after removing selinux file context", "host", h, "path", fc.Path, "error", err)
		}
	}
}

// restoreBooleans puts every recorded boolean back to the value the host held beforehand.
func (p *RemoveSelinuxPolicy) restoreBooleans(ctx context.Context, h *cluster.ZarfHost, state selinuxState) {
	if len(state.Booleans) == 0 {
		return
	}

	names := slices.Sorted(maps.Keys(state.Booleans))
	set := "setsebool -P " + strings.Join(selinuxBooleanArgs(state.Booleans, names), " ")
	logger.From(ctx).Info("restoring selinux booleans", "host", h, "booleans", names)
	if err := p.Wet(h, set, func() error {
		return h.SudoExec(set)
	}); err != nil {
		logger.From(ctx).Warn("could not restore selinux booleans", "host", h, "booleans", names, "error", err)
	}
}

// removeModules uninstalls every recorded module and deletes the CIL source cargoship wrote.
func (p *RemoveSelinuxPolicy) removeModules(ctx context.Context, h *cluster.ZarfHost, state selinuxState) {
	for _, m := range state.Modules {
		remove := fmt.Sprintf("%s -X %d -r %s", SEModule, m.Priority, shellQuote(m.Name))
		logger.From(ctx).Info("removing selinux module", "host", h, "module", m.Name, "priority", m.Priority)
		if err := p.Wet(h, remove, func() error {
			return h.SudoExec(remove)
		}); err != nil {
			logger.From(ctx).Warn("could not remove selinux module", "host", h, "module", m.Name, "error", err)
		}

		path := selinuxModulePath(m.Name)
		if !h.FileExist(path) {
			continue
		}
		if err := p.Wet(h, "delete "+path, func() error {
			return h.DeleteFile(path)
		}); err != nil {
			logger.From(ctx).Warn("could not delete selinux module source", "host", h, "file", path, "error", err)
		}
	}
}

// readSELinuxState reads the record cargoship left on h.
func readSELinuxState(h *cluster.ZarfHost) (selinuxState, error) {
	var state selinuxState

	raw, err := h.ReadFile(SELinuxStateFile)
	if err != nil {
		return state, fmt.Errorf("failed to read %s: %w", SELinuxStateFile, err)
	}
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return state, fmt.Errorf("failed to decode %s: %w", SELinuxStateFile, err)
	}

	return state, nil
}
